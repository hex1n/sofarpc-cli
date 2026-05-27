package contract

import (
	"reflect"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

type indexedServiceTestStore struct {
	classes map[string]javamodel.Class
	indexed []string
	loaded  []string
}

func newIndexedServiceTestStore(classes ...javamodel.Class) *indexedServiceTestStore {
	store := &indexedServiceTestStore{classes: map[string]javamodel.Class{}}
	for _, cls := range classes {
		store.classes[cls.FQN] = cls
		store.indexed = append(store.indexed, cls.FQN)
	}
	return store
}

func (s *indexedServiceTestStore) Class(fqn string) (javamodel.Class, bool) {
	s.loaded = append(s.loaded, fqn)
	cls, ok := s.classes[fqn]
	return cls, ok
}

func (s *indexedServiceTestStore) IndexedClasses() []string {
	return append([]string(nil), s.indexed...)
}

func TestDiscoverServiceInterfaces_FiltersIndexedFQNsBeforeParsing(t *testing.T) {
	store := newIndexedServiceTestStore(
		javamodel.Class{
			FQN:        "com.foo.UserFacade",
			SimpleName: "UserFacade",
			Kind:       javamodel.KindInterface,
			Methods:    []javamodel.Method{{Name: "query"}},
		},
		javamodel.Class{
			FQN:        "com.foo.InternalService",
			SimpleName: "InternalService",
			Kind:       javamodel.KindInterface,
			Methods:    []javamodel.Method{{Name: "run"}},
		},
		javamodel.Class{
			FQN:        "com.foo.UserDTO",
			SimpleName: "UserDTO",
			Kind:       javamodel.KindClass,
		},
	)

	discovery := DiscoverServiceInterfaces(store, ServiceDiscoveryOptions{
		Suffixes:      []string{"Facade"},
		IndexedSource: "sourcecontract",
	})

	if discovery.Source != "sourcecontract" {
		t.Fatalf("source: got %q want sourcecontract", discovery.Source)
	}
	if !reflect.DeepEqual(discovery.Suffixes, []string{"Facade"}) {
		t.Fatalf("suffixes: %#v", discovery.Suffixes)
	}
	if !reflect.DeepEqual(discovery.CandidateServices, []string{"com.foo.UserFacade"}) {
		t.Fatalf("candidateServices: %#v", discovery.CandidateServices)
	}
	if !reflect.DeepEqual(discovery.SelectedServices, []string{"com.foo.UserFacade"}) {
		t.Fatalf("selectedServices: %#v", discovery.SelectedServices)
	}
	if !reflect.DeepEqual(store.loaded, []string{"com.foo.UserFacade"}) {
		t.Fatalf("Class calls should be limited by indexed suffix prefilter, got %#v", store.loaded)
	}
}

func TestDiscoverServiceInterfaces_CustomSuffixAndWildcard(t *testing.T) {
	store := newIndexedServiceTestStore(
		javamodel.Class{
			FQN:        "com.foo.UserFacade",
			SimpleName: "UserFacade",
			Kind:       javamodel.KindInterface,
			Methods:    []javamodel.Method{{Name: "query"}},
		},
		javamodel.Class{
			FQN:        "com.foo.InternalService",
			SimpleName: "InternalService",
			Kind:       javamodel.KindInterface,
			Methods:    []javamodel.Method{{Name: "run"}},
		},
		javamodel.Class{
			FQN:        "com.foo.EmptyFacade",
			SimpleName: "EmptyFacade",
			Kind:       javamodel.KindInterface,
		},
	)

	discovery := DiscoverServiceInterfaces(store, ServiceDiscoveryOptions{Suffixes: []string{"Service"}})
	if !reflect.DeepEqual(discovery.SelectedServices, []string{"com.foo.InternalService"}) {
		t.Fatalf("selected services for Service suffix: %#v", discovery.SelectedServices)
	}

	discovery = DiscoverServiceInterfaces(store, ServiceDiscoveryOptions{Suffixes: []string{"*"}})
	want := []string{"com.foo.InternalService", "com.foo.UserFacade"}
	if !reflect.DeepEqual(discovery.SelectedServices, want) {
		t.Fatalf("selected services for wildcard: got %#v want %#v", discovery.SelectedServices, want)
	}
}

func TestDiscoverServiceInterfaces_NonIndexedStoreReportsContractStoreSource(t *testing.T) {
	store := NewInMemoryStore(javamodel.Class{
		FQN:  "com.foo.UserFacade",
		Kind: javamodel.KindInterface,
		Methods: []javamodel.Method{{
			Name: "query",
		}},
	})

	discovery := DiscoverServiceInterfaces(store, ServiceDiscoveryOptions{StoreSource: "contract-store"})
	if discovery.Source != "contract-store" {
		t.Fatalf("source: got %q want contract-store", discovery.Source)
	}
	if len(discovery.SelectedServices) != 0 || len(discovery.CandidateServices) != 0 {
		t.Fatalf("non-indexed store should not force broad class discovery: %+v", discovery)
	}
}

func TestNormalizeServiceNameSuffixes(t *testing.T) {
	if got := NormalizeServiceNameSuffixes(nil); !reflect.DeepEqual(got, []string{"Facade"}) {
		t.Fatalf("default suffixes: %#v", got)
	}
	got := NormalizeServiceNameSuffixes([]string{" Service ", "", "Facade", "Service"})
	want := []string{"Facade", "Service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suffixes: got %#v want %#v", got, want)
	}
}
