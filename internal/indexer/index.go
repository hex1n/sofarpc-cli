// Package indexer reads the on-disk facade index produced by the Spoon
// indexer subprocess (architecture §6). It exposes a contract.Store
// implementation, so sofarpc_describe and sofarpc_invoke can query
// class metadata without knowing the on-disk layout.
//
// This file covers the reader. The subprocess driver that invokes
// `java -jar spoon-indexer.jar` lives in a sibling file once the jar
// itself exists — the reader is independently useful for tests and for
// indexes produced by out-of-band tooling.
package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

// DirName is the project-relative directory the indexer writes into.
const DirName = ".sofarpc/index"

// MetaFilename is the top-level manifest file the reader keys off.
const MetaFilename = "_index.json"

// Meta is the top-level manifest. Classes maps FQN → shard path, where
// shard paths are relative to DirName.
type Meta struct {
	Version   int               `json:"version"`
	Generated int64             `json:"generated,omitempty"`
	Classes   map[string]string `json:"classes"`
}

// Index is a lazy, thread-safe reader. Shards load on first Class() call
// and stay cached for the life of the Index. Rebuilding the index on
// disk requires reconstructing the Index (cheap: one JSON read).
type Index struct {
	root string
	dir  string
	meta Meta

	mu    sync.Mutex
	cache map[string]facadesemantic.Class
	miss  map[string]struct{}
}

// Load reads <projectRoot>/.sofarpc/index/_index.json and returns an
// Index. A missing manifest returns an explicit error — callers that
// want "no index configured" semantics should branch on os.IsNotExist
// before mapping to errcode.FacadeNotConfigured.
func Load(projectRoot string) (*Index, error) {
	dir := filepath.Join(projectRoot, filepath.FromSlash(DirName))
	metaPath := filepath.Join(dir, MetaFilename)
	body, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read index manifest %q: %w", metaPath, err)
	}
	var meta Meta
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("parse index manifest %q: %w", metaPath, err)
	}
	return &Index{
		root:  projectRoot,
		dir:   dir,
		meta:  meta,
		cache: map[string]facadesemantic.Class{},
		miss:  map[string]struct{}{},
	}, nil
}

// Class implements contract.Store. On a cache miss it reads the shard
// referenced by Meta; on a shard-read error the class is marked as a
// persistent miss so the caller doesn't retry on every invocation.
func (i *Index) Class(fqn string) (facadesemantic.Class, bool) {
	if i == nil {
		return facadesemantic.Class{}, false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if cls, ok := i.cache[fqn]; ok {
		return cls, true
	}
	if _, missed := i.miss[fqn]; missed {
		return facadesemantic.Class{}, false
	}
	rel, ok := i.meta.Classes[fqn]
	if !ok {
		i.miss[fqn] = struct{}{}
		return facadesemantic.Class{}, false
	}
	cls, err := readShard(filepath.Join(i.dir, filepath.FromSlash(rel)))
	if err != nil {
		// A shard referenced by the manifest but unreadable on disk is a
		// stale-index signal. Mark miss so we don't thrash; caller can
		// re-run the indexer to repair.
		i.miss[fqn] = struct{}{}
		return facadesemantic.Class{}, false
	}
	i.cache[fqn] = cls
	return cls, true
}

// Size reports how many classes the manifest lists. Cheap: no shard I/O.
func (i *Index) Size() int {
	if i == nil {
		return 0
	}
	return len(i.meta.Classes)
}

// Services scans shards for Kind=interface entries and returns their
// FQNs. This loads every shard and is intended for one-shot discovery
// (e.g. sofarpc_open's facade banner); callers that want cheap counts
// should use Size().
func (i *Index) Services() []string {
	if i == nil {
		return nil
	}
	out := make([]string, 0, len(i.meta.Classes))
	for fqn := range i.meta.Classes {
		cls, ok := i.Class(fqn)
		if !ok {
			continue
		}
		if cls.Kind == facadesemantic.KindInterface {
			out = append(out, fqn)
		}
	}
	return out
}

func readShard(path string) (facadesemantic.Class, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return facadesemantic.Class{}, err
	}
	var cls facadesemantic.Class
	if err := json.Unmarshal(body, &cls); err != nil {
		return facadesemantic.Class{}, fmt.Errorf("parse shard %q: %w", path, err)
	}
	return cls, nil
}
