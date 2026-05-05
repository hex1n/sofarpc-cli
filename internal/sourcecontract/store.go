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
	mu                   sync.RWMutex
	index                map[string]string
	symbols              symbolTable
	cache                map[string]javamodel.Class
	indexedFiles         int
	indexFailureCount    int
	indexFailures        map[string]string
	duplicateClasses     map[string][]string
	skippedDirs          map[string]int
	generatedSourceFiles int
	moduleRoots          int
	hints                []string
	parseFailureCount    int
	parseFailures        map[string]string
	resolutionIssueCount int
	resolutionIssues     map[string][]string
}

// Diagnostics surfaces the store's current health so sofarpc_open and
// sofarpc_doctor can report how many files were scanned, how many
// parsed successfully, and where the failures clustered.
type Diagnostics struct {
	IndexedClasses       int                 `json:"indexedClasses"`
	IndexedFiles         int                 `json:"indexedFiles"`
	ParsedClasses        int                 `json:"parsedClasses"`
	IndexFailureCount    int                 `json:"indexFailureCount,omitempty"`
	ParseFailureCount    int                 `json:"parseFailureCount,omitempty"`
	ResolutionIssueCount int                 `json:"resolutionIssueCount,omitempty"`
	IndexFailures        map[string]string   `json:"indexFailures,omitempty"`
	ParseFailures        map[string]string   `json:"parseFailures,omitempty"`
	ResolutionIssues     map[string][]string `json:"resolutionIssues,omitempty"`
	DuplicateClasses     map[string][]string `json:"duplicateClasses,omitempty"`
	SkippedDirs          map[string]int      `json:"skippedDirs,omitempty"`
	GeneratedSourceFiles int                 `json:"generatedSourceFiles,omitempty"`
	ModuleRoots          int                 `json:"moduleRoots,omitempty"`
	Hints                []string            `json:"hints,omitempty"`
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

	index, files, stats, err := scanProjectIndex(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(index) == 0 {
		return nil, nil
	}
	return &Store{
		index:                index,
		symbols:              newProjectSymbolTable(index),
		cache:                map[string]javamodel.Class{},
		indexedFiles:         files,
		indexFailureCount:    stats.indexFailureCount,
		indexFailures:        stats.indexFailures,
		duplicateClasses:     stats.duplicateClasses,
		skippedDirs:          stats.skippedDirs,
		generatedSourceFiles: stats.generatedSourceFiles,
		moduleRoots:          len(stats.moduleRoots),
		hints:                stats.hints(),
		parseFailureCount:    0,
		parseFailures:        map[string]string{},
		resolutionIssues:     map[string][]string{},
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

	classes, resolutionIssues, ok := parseJavaFileClasses(path, s.symbols)
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
	for _, issue := range resolutionIssues {
		s.recordResolutionIssueLocked(issue)
	}
	cls, exists := s.cache[fqn]
	if !exists {
		s.recordParseFailureLocked(fqn, "parsed file did not materialize requested class")
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
		IndexedClasses:       len(s.index),
		IndexedFiles:         s.indexedFiles,
		ParsedClasses:        len(s.cache),
		IndexFailureCount:    s.indexFailureCount,
		ParseFailureCount:    s.parseFailureCount,
		ResolutionIssueCount: s.resolutionIssueCount,
		IndexFailures:        cloneStringMap(s.indexFailures),
		ParseFailures:        cloneStringMap(s.parseFailures),
		ResolutionIssues:     cloneStringSliceMap(s.resolutionIssues),
		DuplicateClasses:     cloneStringSliceMap(s.duplicateClasses),
		SkippedDirs:          cloneIntMap(s.skippedDirs),
		GeneratedSourceFiles: s.generatedSourceFiles,
		ModuleRoots:          s.moduleRoots,
		Hints:                append([]string(nil), s.hints...),
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
	s.recordParseFailureLocked(fqn, detail)
}

func (s *Store) recordParseFailureLocked(fqn, detail string) {
	if s.parseFailures == nil {
		s.parseFailures = map[string]string{}
	}
	if _, exists := s.parseFailures[fqn]; exists {
		return
	}
	s.parseFailureCount++
	s.parseFailures[fqn] = detail
}

func (s *Store) recordResolutionIssueLocked(issue typeResolutionIssue) {
	classFQN := strings.TrimSpace(issue.ClassFQN)
	message := strings.TrimSpace(issue.message())
	if classFQN == "" || message == "" {
		return
	}
	if s.resolutionIssues == nil {
		s.resolutionIssues = map[string][]string{}
	}
	for _, existing := range s.resolutionIssues[classFQN] {
		if existing == message {
			return
		}
	}
	s.resolutionIssues[classFQN] = append(s.resolutionIssues[classFQN], message)
	s.resolutionIssueCount++
}
