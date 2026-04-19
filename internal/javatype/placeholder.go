package javatype

import "encoding/json"

// PlaceholderKind labels the JSON skeleton we render for a type. Each kind
// maps to exactly one placeholder literal. The set is intentionally small
// (six entries) — any type we cannot classify falls back to PlaceholderObject,
// which renders `null` and leaves the agent to describe it further.
type PlaceholderKind int

const (
	PlaceholderObject PlaceholderKind = iota
	PlaceholderString
	PlaceholderBool
	PlaceholderNumber
	PlaceholderDecimal
	PlaceholderDate
	PlaceholderCollection
	PlaceholderMap
)

// Placeholder returns the kind of JSON skeleton for fqn. It is independent
// of Classify: a type may be Passthrough for invocation but still need a
// specific literal for describe-time rendering (e.g. LocalDate → "1970-…").
func Placeholder(fqn string) PlaceholderKind {
	base := normalize(fqn)
	if base == "" {
		return PlaceholderObject
	}
	if _, ok := primitives[base]; ok {
		switch base {
		case "boolean":
			return PlaceholderBool
		case "char":
			return PlaceholderString
		case "void":
			return PlaceholderObject
		default:
			return PlaceholderNumber
		}
	}
	if _, ok := stringLike[base]; ok {
		return PlaceholderString
	}
	if _, ok := boolLike[base]; ok {
		return PlaceholderBool
	}
	if _, ok := decimalLike[base]; ok {
		return PlaceholderDecimal
	}
	if _, ok := numberLike[base]; ok {
		return PlaceholderNumber
	}
	if _, ok := dateLike[base]; ok {
		return PlaceholderDate
	}
	if _, ok := mapBases[base]; ok {
		return PlaceholderMap
	}
	if _, ok := containerBases[base]; ok {
		return PlaceholderCollection
	}
	return PlaceholderObject
}

// RenderPlaceholder returns the JSON literal for kind. Kept as a separate
// function from Placeholder so callers can override the literal without
// re-implementing classification.
func RenderPlaceholder(kind PlaceholderKind) json.RawMessage {
	switch kind {
	case PlaceholderString:
		return json.RawMessage(`""`)
	case PlaceholderBool:
		return json.RawMessage(`false`)
	case PlaceholderNumber:
		return json.RawMessage(`0`)
	case PlaceholderDecimal:
		return json.RawMessage(`"0"`)
	case PlaceholderDate:
		return json.RawMessage(`"1970-01-01T00:00:00Z"`)
	case PlaceholderCollection:
		return json.RawMessage(`[]`)
	case PlaceholderMap:
		return json.RawMessage(`{}`)
	default:
		return json.RawMessage(`null`)
	}
}

var stringLike = map[string]struct{}{
	"java.lang.String":       {},
	"java.lang.CharSequence": {},
	"java.lang.Character":    {},
	"java.util.UUID":         {},
}

var boolLike = map[string]struct{}{
	"java.lang.Boolean": {},
}

var numberLike = map[string]struct{}{
	"java.lang.Byte":    {},
	"java.lang.Short":   {},
	"java.lang.Integer": {},
	"java.lang.Long":    {},
	"java.lang.Float":   {},
	"java.lang.Double":  {},
	"java.lang.Number":  {},
}

var decimalLike = map[string]struct{}{
	"java.math.BigDecimal": {},
	"java.math.BigInteger": {},
}

var dateLike = map[string]struct{}{
	"java.util.Date":           {},
	"java.sql.Date":            {},
	"java.sql.Time":            {},
	"java.sql.Timestamp":       {},
	"java.time.Instant":        {},
	"java.time.LocalDate":      {},
	"java.time.LocalDateTime":  {},
	"java.time.LocalTime":      {},
	"java.time.OffsetDateTime": {},
	"java.time.ZonedDateTime":  {},
}

var mapBases = map[string]struct{}{
	"java.util.Map": {},
}
