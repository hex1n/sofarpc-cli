package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Spec is everything Run needs to invoke the Spoon indexer. The CLI
// shape is fixed by architecture §6:
//
//	java -jar spoon-indexer.jar --project <root> --source <dir>... [--since <ms>] --output <dir>
//
// Run writes into <ProjectRoot>/.sofarpc/index; callers that need a
// different layout must adapt the reader too, which isn't worth the
// flexibility right now.
type Spec struct {
	// Java is the absolute path to the `java` binary. Empty defaults to
	// "java" on PATH (convenient for tests and CI, but the MCP
	// entrypoint should resolve SOFARPC_JAVA in production).
	Java string

	// Jar is the absolute path to spoon-indexer.jar. Required.
	Jar string

	// ProjectRoot is the project being indexed. Required. The manifest
	// and shards land under <ProjectRoot>/.sofarpc/index.
	ProjectRoot string

	// Sources are source roots to walk. At least one required.
	Sources []string

	// Since, when non-zero, enables incremental mode — only files
	// modified after this timestamp are reparsed. Zero means full scan.
	Since time.Time

	// ExtraArgs are appended after the fixed flags, for future tuning.
	ExtraArgs []string

	// Env entries to merge onto the child env (KEY=VALUE format).
	Env []string

	// Timeout caps the whole run. Zero defaults to 5m, matching the
	// architecture's worst-case bulk reindex budget.
	Timeout time.Duration
}

// Summary is what Run reports back on success. Classes mirrors
// Index.Size() after re-loading the produced manifest, so callers can
// short-circuit "did the indexer actually find anything?" checks.
type Summary struct {
	Classes     int           `json:"classes"`
	Output      string        `json:"output"`
	Duration    time.Duration `json:"duration"`
	Incremental bool          `json:"incremental"`
}

const defaultIndexerTimeout = 5 * time.Minute

// Run invokes the Spoon indexer synchronously. On success it returns a
// Summary built from the freshly-written manifest; on failure the error
// carries a stable prefix ("indexer: ...") so callers can surface it.
//
// Run does NOT swallow the subprocess's stdout/stderr — both are piped
// to os.Stderr so indexer progress logs end up alongside other server
// diagnostics. Callers that need a quieter run can wrap Spec.Env to
// silence log4j there instead.
func Run(ctx context.Context, spec Spec) (Summary, error) {
	if spec.Jar == "" {
		return Summary{}, errors.New("indexer: Jar is required")
	}
	if spec.ProjectRoot == "" {
		return Summary{}, errors.New("indexer: ProjectRoot is required")
	}
	if len(spec.Sources) == 0 {
		return Summary{}, errors.New("indexer: at least one Source is required")
	}
	java := spec.Java
	if java == "" {
		java = "java"
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = defaultIndexerTimeout
	}
	output := filepath.Join(spec.ProjectRoot, filepath.FromSlash(DirName))
	if err := os.MkdirAll(output, 0o755); err != nil {
		return Summary{}, fmt.Errorf("indexer: prepare output: %w", err)
	}

	args := []string{"-jar", spec.Jar, "--project", spec.ProjectRoot}
	for _, src := range spec.Sources {
		args = append(args, "--source", src)
	}
	args = append(args, "--output", output)
	incremental := !spec.Since.IsZero()
	if incremental {
		args = append(args, "--since", strconv.FormatInt(spec.Since.UnixMilli(), 10))
	}
	args = append(args, spec.ExtraArgs...)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, java, args...)
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)
	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return Summary{}, fmt.Errorf("indexer: timed out after %s", timeout)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return Summary{}, ctx.Err()
		}
		return Summary{}, fmt.Errorf("indexer: run: %w", runErr)
	}

	idx, err := Load(spec.ProjectRoot)
	if err != nil {
		return Summary{}, fmt.Errorf("indexer: load produced index: %w", err)
	}
	return Summary{
		Classes:     idx.Size(),
		Output:      output,
		Duration:    duration,
		Incremental: incremental,
	}, nil
}
