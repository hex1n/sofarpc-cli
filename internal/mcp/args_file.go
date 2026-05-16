package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

const (
	envArgsFileRoot     = "SOFARPC_ARGS_FILE_ROOT"
	envArgsFileMaxBytes = "SOFARPC_ARGS_FILE_MAX_BYTES"
	defaultArgsFileMax  = int64(1 << 20)
)

func decodeJSONValue(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var parsed any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// normalizeArgs normalizes the loosely-typed args field:
//   - nil              -> nil (plan renders a skeleton).
//   - "@<path>" string -> read the file, parse as JSON with UseNumber.
//   - anything else    -> pass through verbatim for BuildPlan to shape-check.
//
// Relative @file paths are resolved against SOFARPC_ARGS_FILE_ROOT when set,
// otherwise against the MCP project root. Absolute paths must still remain
// inside that root after symlink resolution.
//
// service/method are threaded in only to pre-fill the describe hint on
// failure. Empty values are dropped so the agent never sees a hint it cannot
// follow verbatim.
func normalizeArgs(service, method string, raw any, projectRoot string) (any, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if !strings.HasPrefix(v, "@") {
			return v, nil
		}
		path := strings.TrimPrefix(v, "@")
		if path == "" {
			return nil, errcode.New(errcode.ArgsInvalid, "invoke",
				"args '@' prefix requires a file path").
				WithHint("sofarpc_describe", describeHintArgs(service, method),
					"use @<path> rooted at SOFARPC_ARGS_FILE_ROOT or the project root")
		}
		return readArgsFile(service, method, path, projectRoot)
	default:
		return raw, nil
	}
}

// readArgsFile loads path and decodes it as JSON with UseNumber. Errors are
// wrapped into input.args-invalid so the agent sees one shape regardless of
// whether args came inline or from disk.
func readArgsFile(service, method, path, projectRoot string) (any, error) {
	resolved, err := resolveArgsFilePath(path, projectRoot)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("resolve args file %q: %v", path, err)).
			WithHint("sofarpc_doctor", nil, "check SOFARPC_ARGS_FILE_ROOT and the file path")
	}
	maxBytes := argsFileMaxBytes()
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("stat args file %q: %v", resolved, err)).
			WithHint("sofarpc_doctor", nil, "check that the mcp process can read the file")
	}
	if info.IsDir() {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q is a directory", resolved)).
			WithHint("sofarpc_describe", describeHintArgs(service, method), "use a JSON file")
	}
	if info.Size() > maxBytes {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q is %d bytes, over limit %d", resolved, info.Size(), maxBytes)).
			WithHint("sofarpc_describe", describeHintArgs(service, method), "use a smaller JSON file or raise SOFARPC_ARGS_FILE_MAX_BYTES")
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("read args file %q: %v", resolved, err)).
			WithHint("sofarpc_doctor", nil, "check that the mcp process can read the file")
	}
	if int64(len(body)) > maxBytes {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q grew over limit %d", resolved, maxBytes)).
			WithHint("sofarpc_describe", describeHintArgs(service, method), "use a smaller JSON file or raise SOFARPC_ARGS_FILE_MAX_BYTES")
	}
	parsed, err := decodeJSONValue(body)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse args file %q as JSON: %v", resolved, err)).
			WithHint("sofarpc_describe", describeHintArgs(service, method),
				"the file must contain valid JSON matching paramTypes")
	}
	return parsed, nil
}

func resolveArgsFilePath(path, projectRoot string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty file path")
	}
	root := strings.TrimSpace(os.Getenv(envArgsFileRoot))
	if root == "" {
		root = strings.TrimSpace(projectRoot)
	}
	if root == "" {
		return "", fmt.Errorf("args file root is not configured")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve args file root: %w", err)
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes args file root %q", absRoot)
	}
	return resolved, nil
}

func argsFileMaxBytes() int64 {
	raw := strings.TrimSpace(os.Getenv(envArgsFileMaxBytes))
	if raw == "" {
		return defaultArgsFileMax
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultArgsFileMax
	}
	return value
}
