package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// Profile names a worker JVM. Two requests with the same Profile share a
// process; different profiles get separate JVMs (architecture §7.1).
//
// Crucially, Profile does NOT include the stub-jar set — that lives on
// the per-request ClassloaderID instead. This is the change from the
// pre-rewrite design called out in improvement-plan.md §5.
type Profile struct {
	SOFARPCVersion   string
	RuntimeJarDigest string
	JavaMajor        int
}

// Key is the deterministic content-hash used everywhere the pool needs
// to look a worker up. Stable across processes — same inputs → same hex.
func (p Profile) Key() string {
	parts := []string{
		"sofarpc=" + p.SOFARPCVersion,
		"runtime=" + p.RuntimeJarDigest,
		"java=" + strconv.Itoa(p.JavaMajor),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// Empty reports whether the profile has any required field unset. The
// pool refuses to spawn workers from empty profiles so callers fail fast
// instead of starting an unkeyable JVM.
func (p Profile) Empty() bool {
	return p.SOFARPCVersion == "" || p.RuntimeJarDigest == "" || p.JavaMajor == 0
}

// ClassloaderKey hashes the (sorted) stub-jar list into a stable
// identifier the worker uses to cache its URLClassLoader. The Go side
// computes this so it can be passed in Request.Classloader.ID and the
// worker can reuse a previously-cached loader without re-hashing.
func ClassloaderKey(stubJars []string) string {
	if len(stubJars) == 0 {
		return "none"
	}
	sorted := append([]string(nil), stubJars...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "|")))
	return hex.EncodeToString(sum[:])
}
