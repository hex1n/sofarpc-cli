// Package sourcecontract builds a contract.Store directly from Java source
// files under a project root. It exists so a fresh machine can run
// sofarpc_describe without Java, Spoon, or any local cache format.
//
// File layout:
//   - store.go:       public Store API surface (Load, Class, Size, Diagnostics).
//   - scan.go:        project-root walk and lightweight FQN indexing.
//   - parse.go:       Java-source token/structure parsing.
//   - materialize.go: parsed-class → javamodel.Class conversion and tree flattening.
//   - resolver.go:    raw Java type string → canonical FQN resolution.
//   - textutil.go:    primitive byte/string helpers shared by the parser.
package sourcecontract

import (
	"os"
	"strings"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// Store is a lazily materialized contract store backed by Java source files.
// Load builds a light FQN->path index; Class(fqn) parses and caches a class on
// first access.
type Store struct {
	mu            sync.RWMutex
	index         map[string]string
	cache         map[string]javamodel.Class
	indexedFiles  int
	indexFailures map[string]string
	parseFailures map[string]string
}

// Diagnostics surfaces the store's current health so sofarpc_open and
// sofarpc_doctor can report how many files were scanned, how many
// parsed successfully, and where the failures clustered.
type Diagnostics struct {
	IndexedClasses int               `json:"indexedClasses"`
	IndexedFiles   int               `json:"indexedFiles"`
	ParsedClasses  int               `json:"parsedClasses"`
	IndexFailures  map[string]string `json:"indexFailures,omitempty"`
	ParseFailures  map[string]string `json:"parseFailures,omitempty"`
}

// Load scans projectRoot for Java source files and returns a Store when at
// least one top-level type can be indexed. Hidden and build-output directories
// are skipped so editor worktrees and generated trees do not dominate the
// result set. The scan is intentionally shallow: it records only FQN->file
// mappings and defers field/method parsing until Class(fqn) is called.
func Load(projectRoot string) (*Store, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil, nil
	}
	info, err := os.Stat(projectRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	index, files, failures, err := scanProjectIndex(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(index) == 0 {
		return nil, nil
	}
	return &Store{
		index:         index,
		cache:         map[string]javamodel.Class{},
		indexedFiles:  files,
		indexFailures: failures,
		parseFailures: map[string]string{},
	}, nil
}

// Class implements contract.Store. On a cache miss it parses the file
// backing fqn (including any nested declarations) and stores every
// top-level and inner class it finds, so subsequent Class() calls for
// siblings skip the I/O round-trip.
func (s *Store) Class(fqn string) (javamodel.Class, bool) {
	if s == nil {
		return javamodel.Class{}, false
	}
	s.mu.RLock()
	if c, ok := s.cache[fqn]; ok {
		s.mu.RUnlock()
		return c, true
	}
	path, ok := s.findIndexedPathFor(fqn)
	s.mu.RUnlock()
	if !ok {
		return javamodel.Class{}, false
	}

	classes, ok := parseJavaFileClasses(path)
	if !ok || len(classes) == 0 {
		s.recordParseFailure(fqn, "parseJavaFile returned no top-level declaration")
		return javamodel.Class{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, exists := s.cache[fqn]; exists {
		return cached, true
	}
	if s.cache == nil {
		s.cache = map[string]javamodel.Class{}
	}
	for _, cls := range classes {
		s.cache[cls.FQN] = cls
	}
	cls, exists := s.cache[fqn]
	if !exists {
		s.recordParseFailure(fqn, "parsed file did not materialize requested class")
		return javamodel.Class{}, false
	}
	return cls, true
}

// Size returns the number of classes loaded into the store.
func (s *Store) Size() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.index)
}

// Diagnostics snapshots the store's index/cache state for health
// reporting. Maps are cloned so callers cannot mutate internal state.
func (s *Store) Diagnostics() Diagnostics {
	if s == nil {
		return Diagnostics{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Diagnostics{
		IndexedClasses: len(s.index),
		IndexedFiles:   s.indexedFiles,
		ParsedClasses:  len(s.cache),
		IndexFailures:  cloneStringMap(s.indexFailures),
		ParseFailures:  cloneStringMap(s.parseFailures),
	}
}

// findIndexedPathFor returns the file that backs fqn, walking up the
// outer-class chain so Outer.Inner resolves against the file declaring
// Outer. Must be called with s.mu held in a read mode.
func (s *Store) findIndexedPathFor(fqn string) (string, bool) {
	if s == nil {
		return "", false
	}
	if path, ok := s.index[fqn]; ok {
		return path, true
	}
	candidate := fqn
	for {
		cut := strings.LastIndexByte(candidate, '.')
		if cut < 0 {
			return "", false
		}
		candidate = candidate[:cut]
		if path, ok := s.index[candidate]; ok {
			return path, true
		}
	}
}

func (s *Store) recordParseFailure(fqn, detail string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parseFailures == nil {
		s.parseFailures = map[string]string{}
	}
	if _, exists := s.parseFailures[fqn]; exists {
		return
	}
	s.parseFailures[fqn] = detail
}
