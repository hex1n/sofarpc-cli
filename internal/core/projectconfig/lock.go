package projectconfig

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LockProject serializes configuration changes made by tool processes under the same OS user.
func LockProject(root string) (*os.File, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve config lock path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	if runtime.GOOS == "windows" {
		absolute = strings.ToLower(absolute)
	}
	lockDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config lock directory: %w", err)
	}
	lockDir = filepath.Join(lockDir, "sofarpc-cli", "locks")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return nil, fmt.Errorf("create config lock directory: %w", err)
	}
	lockPath := filepath.Join(lockDir, fmt.Sprintf("%x.lock", sha256.Sum256([]byte(absolute))))
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open config lock: %w", err)
	}
	if err := lockFile(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock project config: %w", err)
	}
	return lock, nil
}
