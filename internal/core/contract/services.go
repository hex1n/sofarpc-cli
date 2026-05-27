package contract

import (
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

const DefaultServiceNameSuffix = "Facade"

type IndexedClassStore interface {
	Store
	IndexedClasses() []string
}

type ServiceDiscoveryOptions struct {
	Suffixes      []string
	IndexedSource string
	StoreSource   string
}

type ServiceDiscovery struct {
	Source            string
	Suffixes          []string
	CandidateServices []string
	SelectedServices  []string
}

func DiscoverServiceInterfaces(store Store, opts ServiceDiscoveryOptions) ServiceDiscovery {
	suffixes := NormalizeServiceNameSuffixes(opts.Suffixes)
	out := ServiceDiscovery{Suffixes: suffixes}
	if store == nil {
		return out
	}
	indexed, ok := store.(IndexedClassStore)
	if !ok {
		out.Source = sourceName(opts.StoreSource, "contract-store")
		return out
	}
	out.Source = sourceName(opts.IndexedSource, "indexed-contract-store")
	var candidates []string
	var selected []string
	for _, fqn := range indexed.IndexedClasses() {
		if !nameMatchesSuffix(fqnSimpleName(fqn), suffixes) {
			continue
		}
		cls, ok := store.Class(fqn)
		if !ok || cls.Kind != javamodel.KindInterface || len(cls.Methods) == 0 {
			continue
		}
		candidates = append(candidates, cls.FQN)
		if nameMatchesSuffix(classSimpleName(cls), suffixes) {
			selected = append(selected, cls.FQN)
		}
	}
	out.CandidateServices = normalizeUniqueServiceNames(candidates)
	out.SelectedServices = normalizeUniqueServiceNames(selected)
	return out
}

func NormalizeServiceNameSuffixes(raw []string) []string {
	out := normalizeUniqueServiceNames(raw)
	if len(out) == 0 {
		return []string{DefaultServiceNameSuffix}
	}
	return out
}

func sourceName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func fqnSimpleName(fqn string) string {
	name := strings.TrimSpace(fqn)
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	return name
}

func classSimpleName(cls javamodel.Class) string {
	name := strings.TrimSpace(cls.SimpleName)
	if name != "" {
		return name
	}
	return fqnSimpleName(cls.FQN)
}

func nameMatchesSuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if suffix == "*" || strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func normalizeUniqueServiceNames(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
