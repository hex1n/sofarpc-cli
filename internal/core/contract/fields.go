package contract

import "github.com/hex1n/sofarpc-cli/internal/javamodel"

func ResolvedFields(store Store, fqn string) []javamodel.Field {
	if store == nil || fqn == "" {
		return nil
	}
	var out []javamodel.Field
	index := map[string]int{}
	seen := map[string]bool{}

	var walk func(string)
	walk = func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		cls, ok := store.Class(name)
		if !ok {
			return
		}
		if cls.Superclass != "" {
			walk(rawJavaTypeName(cls.Superclass))
		}
		for _, field := range cls.Fields {
			if i, exists := index[field.Name]; exists {
				out[i] = field
				continue
			}
			index[field.Name] = len(out)
			out = append(out, field)
		}
	}

	walk(fqn)
	return out
}

func resolvedFieldMap(store Store, fqn string) map[string]javamodel.Field {
	fields := ResolvedFields(store, fqn)
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]javamodel.Field, len(fields))
	for _, field := range fields {
		out[field.Name] = field
	}
	return out
}
