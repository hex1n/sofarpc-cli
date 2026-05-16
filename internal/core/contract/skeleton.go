package contract

import (
	"encoding/json"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
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
		out = append(out, buildValue(ParseTypeSpec(pt), store, lookup, map[string]bool{}))
	}
	return out
}

func buildValue(spec TypeSpec, store Store, lookup javatype.ClassLookup, seen map[string]bool) json.RawMessage {
	if spec.IsZero() {
		return json.RawMessage(`null`)
	}
	if spec.Wildcard != WildcardNone {
		spec = spec.Effective()
	}
	if spec.ArrayDepth > 0 {
		inner := buildValue(spec.Element(), store, lookup, seen)
		return arrayOf(inner)
	}
	if isEnumType(spec.Base, store, lookup) {
		return enumSkeleton(spec.Base, store)
	}

	role := javatype.Classify(spec.Base, lookup)
	switch role {
	case javatype.RolePassthrough:
		if spec.Base == "long" || spec.Base == "java.lang.Long" {
			// MCP clients frequently round JSON numbers above 2^53-1. Use a
			// decimal string placeholder so agents naturally send Long values in
			// the only representation that round-trips safely through the host.
			return json.RawMessage(`"0"`)
		}
		if spec.Base == "java.math.BigDecimal" || spec.Base == "java.math.BigInteger" {
			return decimalObject(spec.Base)
		}
		return javatype.RenderPlaceholder(javatype.Placeholder(spec.Base))
	case javatype.RoleContainer:
		return buildContainer(spec, store, lookup, seen)
	case javatype.RoleUserType:
		if seen[spec.Base] {
			return stubObject(spec.Base)
		}
		seen[spec.Base] = true
		defer delete(seen, spec.Base)
		return buildUserType(spec.Base, store, lookup, seen)
	default:
		return json.RawMessage(`null`)
	}
}

// buildContainer renders a one-element sample for declared generic
// containers. Map uses "<key>" as a human-readable placeholder key; the
// value is a real recursive skeleton so nested DTOs are visible.
// Containers without declared type args fall back to `[]` / `{}` —
// some contract sources may lose parameters at erasure boundaries, and an
// empty placeholder beats wrong guessing.
func buildContainer(spec TypeSpec, store Store, lookup javatype.ClassLookup, seen map[string]bool) json.RawMessage {
	if javatype.Placeholder(spec.Base) == javatype.PlaceholderMap {
		if len(spec.Args) < 2 {
			return json.RawMessage(`{}`)
		}
		valueSkel := buildValue(spec.Args[1], store, lookup, seen)
		return json.RawMessage(`{"<key>":` + string(valueSkel) + `}`)
	}
	if len(spec.Args) < 1 {
		return json.RawMessage(`[]`)
	}
	elemSkel := buildValue(spec.Args[0], store, lookup, seen)
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
	if cls.Kind == javamodel.KindEnum {
		return enumSkeleton(fqn, store)
	}

	obj := newOrderedObject()
	obj.put("@type", json.RawMessage(`"`+fqn+`"`))
	for _, f := range ResolvedFields(store, fqn) {
		obj.put(f.Name, buildValue(ParseTypeSpec(f.JavaType), store, lookup, seen))
	}
	return obj.marshal()
}

func stubObject(fqn string) json.RawMessage {
	return json.RawMessage(`{"@type":"` + fqn + `"}`)
}

func decimalObject(fqn string) json.RawMessage {
	return json.RawMessage(`{"@type":"` + fqn + `","value":"0"}`)
}

func enumSkeleton(fqn string, store Store) json.RawMessage {
	name := ""
	if cls, ok := classFor(store, fqn); ok && len(cls.EnumConstants) > 0 {
		name = cls.EnumConstants[0]
	}
	nameJSON, err := json.Marshal(name)
	if err != nil {
		nameJSON = []byte(`""`)
	}
	return json.RawMessage(`{"@type":"` + fqn + `","name":` + string(nameJSON) + `}`)
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
