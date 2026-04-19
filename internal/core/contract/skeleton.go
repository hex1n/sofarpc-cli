package contract

import (
	"encoding/json"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/javatype"
)

// BuildSkeleton renders one JSON value per paramType. UserType classes
// get an object with a leading "@type" key so Hessian2 on the remote side
// can pick the correct Java class; containers render with their declared
// element / value type expanded; arrays wrap the element skeleton in
// `[...]`; primitives/strings/dates render via
// javatype.RenderPlaceholder.
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
	t := strings.TrimSpace(javaType)
	if t == "" {
		return json.RawMessage(`null`)
	}

	// Wildcards resolve to their bound so the skeleton shows the agent
	// the actual type they can fill in. `?` degrades to Object, which
	// renders as null — not great, but better than guessing a type.
	if strings.HasPrefix(t, "?") {
		return buildValue(resolveWildcard(t), store, lookup, seen)
	}

	// Arrays recurse element-first and wrap the result. Multidimensional
	// arrays (`Foo[][]`) unwind one dimension at a time.
	if strings.HasSuffix(t, "[]") {
		inner := buildValue(strings.TrimSuffix(t, "[]"), store, lookup, seen)
		return arrayOf(inner)
	}

	base, typeArgs := parseGenerics(t)

	role := javatype.Classify(base, lookup)
	switch role {
	case javatype.RolePassthrough:
		return javatype.RenderPlaceholder(javatype.Placeholder(base))
	case javatype.RoleContainer:
		return buildContainer(base, typeArgs, store, lookup, seen)
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

// buildContainer renders a one-element sample for declared generic
// containers. Map uses "<key>" as a human-readable placeholder key; the
// value is a real recursive skeleton so nested DTOs are visible.
// Containers without declared type args fall back to `[]` / `{}` —
// indexers occasionally lose parameters at erasure boundaries, and an
// empty placeholder beats wrong guessing.
func buildContainer(base string, typeArgs []string, store Store, lookup javatype.ClassLookup, seen map[string]bool) json.RawMessage {
	if javatype.Placeholder(base) == javatype.PlaceholderMap {
		if len(typeArgs) < 2 {
			return json.RawMessage(`{}`)
		}
		valueSkel := buildValue(typeArgs[1], store, lookup, seen)
		return json.RawMessage(`{"<key>":` + string(valueSkel) + `}`)
	}
	if len(typeArgs) < 1 {
		return json.RawMessage(`[]`)
	}
	elemSkel := buildValue(typeArgs[0], store, lookup, seen)
	return arrayOf(elemSkel)
}

func arrayOf(inner json.RawMessage) json.RawMessage {
	out := make([]byte, 0, len(inner)+2)
	out = append(out, '[')
	out = append(out, inner...)
	out = append(out, ']')
	return out
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

// parseGenerics splits `Foo<A, B<C, D>>` into `Foo` + `[A, B<C, D>]`.
// Nested `<...>` groups are balanced by a depth counter so inner commas
// do not accidentally split the outer list. Malformed input (no closing
// `>`) degrades to "treat as bare name" rather than panicking —
// indexers should never emit that, but the skeleton path must not fail
// catastrophically on bad data.
func parseGenerics(s string) (string, []string) {
	s = strings.TrimSpace(s)
	lt := strings.IndexByte(s, '<')
	if lt < 0 {
		return s, nil
	}
	base := strings.TrimSpace(s[:lt])

	depth := 1
	end := -1
	for i := lt + 1; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return base, nil
	}
	return base, splitTopLevelCommas(s[lt+1 : end])
}

// splitTopLevelCommas walks a comma-separated list, tracking `<>` depth
// so commas inside nested generic arguments stay attached to their
// group. Whitespace around each segment is trimmed.
func splitTopLevelCommas(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	tail := strings.TrimSpace(s[start:])
	if tail != "" {
		out = append(out, tail)
	}
	return out
}

// resolveWildcard normalises `?`, `? extends T`, and `? super T` to a
// concrete type the skeleton can render. `? super T` takes T (the lower
// bound) because the request payload is producer-side: the agent will
// put a T in. Bare `?` degrades to Object, which renders as null.
func resolveWildcard(s string) string {
	s = strings.TrimSpace(s)
	if s == "?" {
		return "java.lang.Object"
	}
	if rest, ok := trimPrefix(s, "? extends "); ok {
		return strings.TrimSpace(rest)
	}
	if rest, ok := trimPrefix(s, "? super "); ok {
		return strings.TrimSpace(rest)
	}
	return "java.lang.Object"
}

func trimPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
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
