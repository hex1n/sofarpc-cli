// Package contract resolves a method's overload and renders a JSON
// skeleton the agent can populate before calling sofarpc_invoke. It is
// the only package that joins facadesemantic (raw class data) with
// javatype (type classification) — handlers should not import either
// directly when the decision is "which overload + what payload shape".
//
// The Store interface is the single seam the rest of the system plugs
// into. Tests use InMemoryStore; production code can materialise contract
// data from any source without changing the resolver.
package contract

import (
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/javatype"
)

// Store returns class metadata by FQN. It MUST return ok=false for
// unknown types rather than a zero-valued Class — callers rely on the
// flag to decide whether to fall back to another source.
type Store interface {
	Class(fqn string) (facadesemantic.Class, bool)
}

// InMemoryStore is the test / scaffolding implementation.
type InMemoryStore struct {
	classes map[string]facadesemantic.Class
}

// NewInMemoryStore seeds a store with the supplied classes.
func NewInMemoryStore(classes ...facadesemantic.Class) *InMemoryStore {
	s := &InMemoryStore{classes: map[string]facadesemantic.Class{}}
	for _, c := range classes {
		s.Put(c)
	}
	return s
}

// Put adds or replaces a class. Keyed on FQN.
func (s *InMemoryStore) Put(class facadesemantic.Class) {
	if s.classes == nil {
		s.classes = map[string]facadesemantic.Class{}
	}
	s.classes[class.FQN] = class
}

// Class implements Store.
func (s *InMemoryStore) Class(fqn string) (facadesemantic.Class, bool) {
	if s == nil || s.classes == nil {
		return facadesemantic.Class{}, false
	}
	c, ok := s.classes[fqn]
	return c, ok
}

// ClassLookup adapts a Store to javatype.ClassLookup so javatype.Classify
// can walk facadesemantic data without importing this package.
func ClassLookup(store Store) javatype.ClassLookup {
	if store == nil {
		return nilLookup{}
	}
	return storeLookup{store: store}
}

type storeLookup struct {
	store Store
}

func (l storeLookup) Superclass(fqn string) (string, bool) {
	c, ok := l.store.Class(fqn)
	if !ok {
		return "", false
	}
	if c.Superclass == "" {
		return "", false
	}
	return c.Superclass, true
}

func (l storeLookup) Interfaces(fqn string) ([]string, bool) {
	c, ok := l.store.Class(fqn)
	if !ok {
		return nil, false
	}
	if len(c.Interfaces) == 0 {
		return nil, false
	}
	return c.Interfaces, true
}

type nilLookup struct{}

func (nilLookup) Superclass(string) (string, bool) { return "", false }
func (nilLookup) Interfaces(string) ([]string, bool) { return nil, false }
