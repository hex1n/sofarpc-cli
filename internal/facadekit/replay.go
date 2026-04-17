package facadekit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ReplayOptions struct {
	Filter          string
	OnlyNames       []string
	ContextOverride string
	DryRun          bool
	Save            bool
	SofaRPCBin      string
	Now             func() time.Time
}

type ReplayFile struct {
	Service string       `json:"service"`
	Method  string       `json:"method"`
	Calls   []ReplaySpec `json:"calls"`
}

type ReplaySpec struct {
	Name        string          `json:"name"`
	Notes       string          `json:"notes,omitempty"`
	Context     string          `json:"context"`
	PayloadMode string          `json:"payloadMode"`
	TimeoutMS   *int            `json:"timeoutMs"`
	Params      json.RawMessage `json:"params"`
}

type replayRow struct {
	ServiceMethod string
	Name          string
	Status        string
	Code          string
	Message       string
}

type exitError struct {
	message string
	silent  bool
}

func (e *exitError) Error() string {
	return e.message
}

func (e *exitError) Silent() bool {
	return e.silent
}

var runReplayCommand = defaultRunReplayCommand

func ReplayCalls(projectRoot string, opts ReplayOptions, stdout, stderr io.Writer) error {
	cfg, err := LoadConfig(projectRoot, false)
	if err != nil {
		return err
	}
	sofarpcBin := firstNonEmpty(strings.TrimSpace(opts.SofaRPCBin), strings.TrimSpace(cfg.SofaRPCBin), "sofarpc")
	replayDir := EffectiveReplayDir(projectRoot)
	replayFiles, err := iterReplayFiles(replayDir)
	if err != nil {
		return err
	}
	if len(replayFiles) == 0 {
		if stderr != nil {
			fmt.Fprintf(stderr, "[replay] no saved calls under %s\n", displayPath(projectRoot, replayDir))
		}
		return &exitError{silent: true}
	}
	runsDir := filepath.Join(replayDir, "_runs")
	if opts.Save {
		if err := os.MkdirAll(runsDir, 0o755); err != nil {
			return err
		}
	}

	onlyNames := map[string]struct{}{}
	for _, name := range opts.OnlyNames {
		name = strings.TrimSpace(name)
		if name != "" {
			onlyNames[name] = struct{}{}
		}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	var rows []replayRow
	anyRPCFail := false
	for _, replayFilePath := range replayFiles {
		payload, err := readReplayFile(replayFilePath, stderr)
		if err != nil {
			continue
		}
		if payload.Service == "" || payload.Method == "" {
			continue
		}
		slug := payload.Service + "." + payload.Method
		if opts.Filter != "" && !strings.Contains(strings.ToLower(slug), strings.ToLower(opts.Filter)) {
			continue
		}
		for _, call := range payload.Calls {
			name := firstNonEmpty(strings.TrimSpace(call.Name), "<unnamed>")
			if len(onlyNames) > 0 {
				if _, ok := onlyNames[name]; !ok {
					continue
				}
			}
			argv, tempPath, err := buildReplayCommand(sofarpcBin, payload.Service, payload.Method, call, opts.ContextOverride, cfg.DefaultContext, projectRoot)
			if err != nil {
				return err
			}
			shortName := shortServiceMethod(payload.Service, payload.Method)
			if stdout != nil {
				fmt.Fprintf(stdout, "▶ %s [%s]\n", shortName, name)
				fmt.Fprintf(stdout, "    %s\n", quoteCommand(argv))
			}
			if opts.DryRun {
				_ = os.Remove(tempPath)
				rows = append(rows, replayRow{
					ServiceMethod: shortName,
					Name:          name,
					Status:        "DRY",
				})
				continue
			}

			rc, stdoutText, stderrText, err := runReplayCommand(argv[0], argv[1:], projectRoot)
			_ = os.Remove(tempPath)
			if err != nil {
				return fmt.Errorf("replay %s [%s]: %w", shortName, name, err)
			}
			summary := parseReplayResult(stdoutText)
			status := classifyReplayResult(rc, summary)
			if status == "RPC_FAIL" {
				anyRPCFail = true
			}
			rows = append(rows, replayRow{
				ServiceMethod: shortName,
				Name:          name,
				Status:        status,
				Code:          stringify(summary["errorCode"]),
				Message:       trimMessage(summary["errorMsg"]),
			})
			if opts.Save {
				out := map[string]any{
					"at":         now().Local().Format(time.RFC3339),
					"status":     status,
					"returnCode": rc,
					"summary":    summary,
					"stdout":     stdoutText,
					"stderr":     stderrText,
				}
				stem := strings.TrimSuffix(filepath.Base(replayFilePath), filepath.Ext(replayFilePath))
				safeName := strings.NewReplacer("/", "_", "\\", "_").Replace(name)
				if err := SaveJSON(filepath.Join(runsDir, stem+"__"+safeName+".json"), out); err != nil {
					return err
				}
			}
		}
	}

	printReplaySummary(stdout, rows)
	if len(rows) == 0 {
		return &exitError{silent: true}
	}
	if anyRPCFail {
		return &exitError{silent: true}
	}
	return nil
}

func iterReplayFiles(replayDir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(replayDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	visible := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(filepath.Base(entry), "_") {
			continue
		}
		visible = append(visible, entry)
	}
	return visible, nil
}

func readReplayFile(path string, stderr io.Writer) (ReplayFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "[replay] skip %s: %v\n", filepath.Base(path), err)
		}
		return ReplayFile{}, err
	}
	var payload ReplayFile
	if err := json.Unmarshal(body, &payload); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "[replay] skip %s: %v\n", filepath.Base(path), err)
		}
		return ReplayFile{}, err
	}
	return payload, nil
}

func buildReplayCommand(sofarpcBin, service, method string, call ReplaySpec, contextOverride, defaultContext, projectRoot string) ([]string, string, error) {
	params := call.Params
	if len(bytes.TrimSpace(params)) == 0 || string(bytes.TrimSpace(params)) == "null" {
		params = json.RawMessage("[]")
	}
	if !json.Valid(params) {
		return nil, "", fmt.Errorf("saved call %s.%s has invalid params JSON", service, method)
	}

	tempFile, err := os.CreateTemp(projectRoot, ".rpc-run-*.json")
	if err != nil {
		return nil, "", err
	}
	if _, err := tempFile.Write(params); err != nil {
		tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return nil, "", err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempFile.Name())
		return nil, "", err
	}

	argv := []string{sofarpcBin, "call"}
	if ctx := firstNonEmpty(strings.TrimSpace(contextOverride), strings.TrimSpace(call.Context), strings.TrimSpace(defaultContext)); ctx != "" {
		argv = append(argv, "-context", ctx)
	}
	if payloadMode := strings.TrimSpace(call.PayloadMode); payloadMode != "" {
		argv = append(argv, "-payload-mode", payloadMode)
	}
	if call.TimeoutMS != nil && *call.TimeoutMS > 0 {
		argv = append(argv, "-timeout-ms", fmt.Sprintf("%d", *call.TimeoutMS))
	}
	argv = append(argv, "-data", "@"+tempFile.Name(), "-full-response", service+"."+method)
	return argv, tempFile.Name(), nil
}

func defaultRunReplayCommand(bin string, args []string, cwd string) (int, string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.String(), stderr.String(), nil
	}
	return 0, stdout.String(), stderr.String(), err
}

func parseReplayResult(stdout string) map[string]any {
	var data any
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return map[string]any{
			"parsed":     false,
			"stdoutHead": truncate(stdout, 200),
		}
	}
	out := map[string]any{"parsed": true}
	root, ok := data.(map[string]any)
	if !ok {
		return out
	}
	body := any(root)
	if result, ok := root["result"].(map[string]any); ok {
		body = result
	}
	if envelope, ok := body.(map[string]any); ok {
		if fields, ok := envelope["fields"].(map[string]any); ok && envelope["type"] != nil {
			out["envelope"] = envelope["type"]
			body = unwrapGenericEnvelope(fields)
		}
	}
	if payload, ok := body.(map[string]any); ok {
		if success, ok := payload["success"]; ok {
			out["success"] = success
		}
		if errorCode := firstNonEmpty(stringify(payload["errorCode"]), stringify(payload["code"])); errorCode != "" {
			out["errorCode"] = errorCode
		}
		if errorMsg := firstNonEmpty(stringify(payload["errorMsg"]), stringify(payload["message"])); errorMsg != "" {
			out["errorMsg"] = errorMsg
		}
	}
	if diagnostics, ok := root["diagnostics"].(map[string]any); ok {
		if target := firstNonEmpty(stringify(diagnostics["targetUrl"]), stringify(diagnostics["target"])); target != "" {
			out["target"] = target
		}
	}
	return out
}

func unwrapGenericEnvelope(body any) any {
	current := body
	for i := 0; i < 3; i++ {
		envelope, ok := current.(map[string]any)
		if !ok {
			break
		}
		fields, ok := envelope["fields"].(map[string]any)
		if !ok || envelope["type"] == nil {
			break
		}
		current = fields
	}
	return current
}

func classifyReplayResult(returnCode int, summary map[string]any) string {
	if returnCode != 0 {
		return "RPC_FAIL"
	}
	if success, ok := summary["success"].(bool); ok {
		if success {
			return "OK"
		}
		return "BIZ_FAIL"
	}
	return "UNKNOWN"
}

func printReplaySummary(stdout io.Writer, rows []replayRow) {
	if stdout == nil {
		return
	}
	fmt.Fprintf(stdout, "\n── summary %s\n", strings.Repeat("─", 60))
	fmt.Fprintf(stdout, "%-50s%-14s%-10s%-10s%s\n", "call", "name", "status", "code", "msg")
	for _, row := range rows {
		fmt.Fprintf(stdout, "%-50s%-14s%-10s%-10s%s\n",
			truncate(row.ServiceMethod, 48),
			truncate(row.Name, 12),
			row.Status,
			truncate(row.Code, 10),
			row.Message,
		)
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "(no saved calls matched filter)")
	}
}

func shortServiceMethod(service, method string) string {
	shortService := service
	if idx := strings.LastIndex(service, "."); idx >= 0 && idx+1 < len(service) {
		shortService = service[idx+1:]
	}
	return shortService + "." + method
}

func quoteCommand(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, quoteArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quoteArg(value string) string {
	if value == "" || strings.ContainsAny(value, " \"'<>|&") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func trimMessage(value any) string {
	return truncate(strings.TrimSpace(stringify(value)), 60)
}

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}
