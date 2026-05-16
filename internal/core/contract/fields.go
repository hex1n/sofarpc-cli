package contract

import "github.com/hex1n/sofarpc-cli/internal/javamodel"

func ResolvedFields(store Store, fqn string) []javamodel.Field {
	if store == nil || fqn == "" {
		return nil
	}
	var out []javamodel.Field
	index := map[string]int{}
	seen := map[string]bool{}

	var walk func(javamodel.Class, map[string]string)
	walk = func(cls javamodel.Class, bindings map[string]string) {
		seenKey := typeBindingKey(cls.FQN, bindings)
		if cls.FQN == "" || seen[seenKey] {
			return
		}
		seen[seenKey] = true
		if cls.Superclass != "" {
			superRef := substituteTypeParams(cls.Superclass, bindings)
			if super, ok := store.Class(rawJavaTypeName(superRef)); ok {
				walk(super, typeArgBindings(superRef, super))
			}
		}
		for _, field := range cls.Fields {
			field = substituteFieldTypeParams(field, bindings)
			if i, exists := index[field.Name]; exists {
				out[i] = field
				continue
			}
			index[field.Name] = len(out)
			out = append(out, field)
		}
	}

	cls, ok := store.Class(fqn)
	if ok {
		walk(cls, nil)
	}
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
