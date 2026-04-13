package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func newDescribeTestApp(t *testing.T) *App {
	t.Helper()
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	return &App{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
}

func TestRunDescribeRequiresSingleFQCN(t *testing.T) {
	app := newDescribeTestApp(t)
	if err := app.runDescribe(nil); err == nil {
		t.Fatal("expected arity error with no positional")
	}
	if err := app.runDescribe([]string{"a", "b"}); err == nil {
		t.Fatal("expected arity error with two positionals")
	}
}
