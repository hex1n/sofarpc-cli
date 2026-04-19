package contract

import (
	"encoding/json"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/javatype"
)

// BuildSkeleton renders one JSON value per paramType. UserType classes
// get an object with a leading "@type" key so Hessian2 on the remote side
// can pick the correct Java class; containers render as [] / {}; known
// primitives/strings/dates render via javatype.RenderPlaceholder.
//
// The store is consulted to walk user-type field types recursively. A
// cycle guard (seen[fqn]) collapses recursive references to a minimal
// {"@type":"..."} stub.
func BuildSkeleton(paramTypes []string, store Store) []json.RawMessage {
	lookup := ClassLookup(store)
	out := make([]json.RawMessage, 0, len(paramTypes))
	for _, pt := range paramTypes {
		out = append(out, buildValue(pt, store, lookup, map[string]bool{}))
	}
	return out
}

func buildValue(javaType string, store Store, lookup javatype.ClassLookup, seen map[string]bool) json.RawMessage {
	base := stripGenerics(javaType)
	base = strings.TrimSuffix(base, "[]")

	role := javatype.Classify(base, lookup)
	switch role {
	case javatype.RolePassthrough:
		return javatype.RenderPlaceholder(javatype.Placeholder(base))
	case javatype.RoleContainer:
		return javatype.RenderPlaceholder(javatype.Placeholder(base))
	case javatype.RoleUserType:
		if seen[base] {
			return stubObject(base)
		}
		seen[base] = true
		defer delete(seen, base)
		return buildUserType(base, store, lookup, seen)
	default:
		return json.RawMessage(`null`)
	}
}

func buildUserType(fqn string, store Store, lookup javatype.ClassLookup, seen map[string]bool) json.RawMessage {
	cls, ok := store.Class(fqn)
	if !ok {
		return stubObject(fqn)
	}
	if cls.Kind == facadesemantic.KindEnum {
		if len(cls.EnumConstants) > 0 {
			body, err := json.Marshal(cls.EnumConstants[0])
			if err == nil {
				return body
			}
		}
		return json.RawMessage(`""`)
	}

	obj := newOrderedObject()
	obj.put("@type", json.RawMessage(`"`+fqn+`"`))
	for _, f := range cls.Fields {
		obj.put(f.Name, buildValue(f.JavaType, store, lookup, seen))
	}
	return obj.marshal()
}

func stubObject(fqn string) json.RawMessage {
	return json.RawMessage(`{"@type":"` + fqn + `"}`)
}

// stripGenerics removes a trailing `<...>` parameter list. Nested
// generics are handled by a simple depth counter.
func stripGenerics(s string) string {
	idx := strings.IndexByte(s, '<')
	if idx < 0 {
		return s
	}
	return s[:idx]
}

// orderedObject lets us emit keys in insertion order so @type always
// appears first — downstream Hessian2 parsers don't require it, but it
// keeps the skeleton readable for humans debugging.
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func newOrderedObject() *orderedObject {
	return &orderedObject{values: map[string]json.RawMessage{}}
}

func (o *orderedObject) put(k string, v json.RawMessage) {
	if _, exists := o.values[k]; !exists {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

func (o *orderedObject) marshal() json.RawMessage {
	if len(o.keys) == 0 {
		return json.RawMessage(`{}`)
	}
	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(k)
		buf.Write(keyJSON)
		buf.WriteByte(':')
		buf.Write(o.values[k])
	}
	buf.WriteByte('}')
	return json.RawMessage(buf.String())
}
