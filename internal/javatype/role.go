// Package javatype classifies a Java type for two decisions we make
// repeatedly: do we need an @type tag when generic-invoking (contract side),
// and what JSON placeholder do we render when describing a method (schema
// side). Today both decisions live in ~12 scattered string whitelists; this
// package centralises them.
//
// The package has no external Go dependencies; callers pass a tiny
// ClassLookup so we stay free of facadesemantic and can be unit-tested
// with hand-built fakes.
package javatype

// Role names the invocation-time treatment of a type.
//
//   - UserType    the agent must attach an @type tag when the value is nested
//     inside a generic container (Object field, List<?>, Map<?,?>).
//     Plain DTOs, custom exceptions, @JsonSubTypes bases, etc.
//   - Container   the type is a Collection or Map. Child values still need
//     @type decisions; the container itself is transparent to Hessian2.
//   - Passthrough the type is a primitive, wrapper, String, Number, Date,
//     Temporal, CharSequence, or enum. Never needs @type.
type Role int

const (
	RoleUnknown Role = iota
	RolePassthrough
	RoleContainer
	RoleUserType
)

func (r Role) String() string {
	switch r {
	case RolePassthrough:
		return "passthrough"
	case RoleContainer:
		return "container"
	case RoleUserType:
		return "user-type"
	default:
		return "unknown"
	}
}

// ClassLookup exposes the superclass / interface chain of a user-space
// Java type. Implementations must return empty strings / empty slices for
// types they do not know about; they MUST NOT panic.
type ClassLookup interface {
	Superclass(fqn string) (string, bool)
	Interfaces(fqn string) ([]string, bool)
}

// Classify returns the Role of fqn by walking the supertype chain. Unknown
// types (not in the registry, not in any built-in set) are treated as
// UserType — the safer default for @type injection, since a missing tag
// causes Hessian2 deserialisation failures at the server, while a redundant
// tag is usually tolerated.
//
// The walk terminates when any ancestor matches one of the built-in sets:
//
//	passthrough:  primitives, wrappers, Number, CharSequence, Date, Temporal,
//	              Enum, UUID, BigInteger, BigDecimal
//	container:    Collection, Map (and their subtypes)
//
// Arrays (trailing "[]") and parameterised names ("List<String>") are
// normalised by stripping the suffix before lookup.
func Classify(fqn string, lookup ClassLookup) Role {
	base := normalize(fqn)
	if base == "" {
		return RoleUnknown
	}
	if _, ok := primitives[base]; ok {
		return RolePassthrough
	}
	if role, ok := builtinRole(base); ok {
		return role
	}
	return walk(base, lookup)
}

func walk(fqn string, lookup ClassLookup) Role {
	if lookup == nil {
		return RoleUserType
	}
	seen := map[string]struct{}{}
	frontier := []string{fqn}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		if current == "" {
			continue
		}
		if _, visited := seen[current]; visited {
			continue
		}
		seen[current] = struct{}{}
		if role, ok := builtinRole(current); ok {
			return role
		}
		if super, ok := lookup.Superclass(current); ok && super != "" {
			frontier = append(frontier, normalize(super))
		}
		if ifaces, ok := lookup.Interfaces(current); ok {
			for _, iface := range ifaces {
				if iface != "" {
					frontier = append(frontier, normalize(iface))
				}
			}
		}
	}
	return RoleUserType
}

func builtinRole(fqn string) (Role, bool) {
	if _, ok := containerBases[fqn]; ok {
		return RoleContainer, true
	}
	if _, ok := passthroughBases[fqn]; ok {
		return RolePassthrough, true
	}
	return RoleUnknown, false
}

// normalize strips array suffix and generic parameters; case-sensitive.
func normalize(fqn string) string {
	out := fqn
	for len(out) > 2 && out[len(out)-2:] == "[]" {
		out = out[:len(out)-2]
	}
	if idx := indexByte(out, '<'); idx >= 0 {
		out = out[:idx]
	}
	return trim(out)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

var primitives = map[string]struct{}{
	"boolean": {}, "byte": {}, "short": {}, "int": {},
	"long": {}, "float": {}, "double": {}, "char": {},
	"void": {},
}

// passthroughBases intentionally omits java.lang.Object: every class walk
// terminates there, so treating Object as passthrough would misclassify all
// DTOs.
var passthroughBases = map[string]struct{}{
	"java.lang.String":            {},
	"java.lang.CharSequence":      {},
	"java.lang.Boolean":           {},
	"java.lang.Byte":              {},
	"java.lang.Short":             {},
	"java.lang.Integer":           {},
	"java.lang.Long":              {},
	"java.lang.Float":             {},
	"java.lang.Double":            {},
	"java.lang.Character":         {},
	"java.lang.Number":            {},
	"java.lang.Enum":              {},
	"java.math.BigInteger":        {},
	"java.math.BigDecimal":        {},
	"java.util.Date":              {},
	"java.sql.Date":               {},
	"java.sql.Time":               {},
	"java.sql.Timestamp":          {},
	"java.util.UUID":              {},
	"java.time.Instant":           {},
	"java.time.LocalDate":         {},
	"java.time.LocalDateTime":     {},
	"java.time.LocalTime":         {},
	"java.time.OffsetDateTime":    {},
	"java.time.ZonedDateTime":     {},
	"java.time.Duration":          {},
	"java.time.Period":            {},
	"java.time.temporal.Temporal": {},
}

var containerBases = map[string]struct{}{
	"java.util.Collection": {},
	"java.util.List":       {},
	"java.util.Set":        {},
	"java.util.Queue":      {},
	"java.util.Deque":      {},
	"java.util.Map":        {},
	"java.lang.Iterable":   {},
}
