package contract

import "strings"

type WildcardKind int

const (
	WildcardNone WildcardKind = iota
	WildcardAny
	WildcardExtends
	WildcardSuper
)

// TypeSpec is the parsed form of a Java type expression used by both
// skeleton rendering and invoke-time argument normalization.
type TypeSpec struct {
	Base       string
	Args       []TypeSpec
	ArrayDepth int
	Wildcard   WildcardKind
}

func ParseTypeSpec(s string) TypeSpec {
	s = strings.TrimSpace(s)
	if s == "" {
		return TypeSpec{}
	}
	if s == "?" {
		return TypeSpec{Base: "java.lang.Object", Wildcard: WildcardAny}
	}
	if rest, ok := trimPrefix(s, "? extends "); ok {
		spec := ParseTypeSpec(rest)
		spec.Wildcard = WildcardExtends
		if spec.Base == "" {
			spec.Base = "java.lang.Object"
		}
		return spec
	}
	if rest, ok := trimPrefix(s, "? super "); ok {
		spec := ParseTypeSpec(rest)
		spec.Wildcard = WildcardSuper
		if spec.Base == "" {
			spec.Base = "java.lang.Object"
		}
		return spec
	}

	spec := TypeSpec{}
	for strings.HasSuffix(s, "[]") {
		spec.ArrayDepth++
		s = strings.TrimSpace(strings.TrimSuffix(s, "[]"))
	}

	base, args := splitGenericArgs(s)
	spec.Base = base
	if len(args) == 0 {
		return spec
	}
	spec.Args = make([]TypeSpec, 0, len(args))
	for _, arg := range args {
		spec.Args = append(spec.Args, ParseTypeSpec(arg))
	}
	return spec
}

func (t TypeSpec) Effective() TypeSpec {
	t.Wildcard = WildcardNone
	if t.Base == "" {
		t.Base = "java.lang.Object"
	}
	return t
}

func (t TypeSpec) Element() TypeSpec {
	if t.ArrayDepth > 0 {
		t.ArrayDepth--
		return t
	}
	if len(t.Args) == 0 {
		return TypeSpec{}
	}
	return t.Args[0]
}

func (t TypeSpec) IsZero() bool {
	return t.Base == "" && len(t.Args) == 0 && t.ArrayDepth == 0 && t.Wildcard == WildcardNone
}

func splitGenericArgs(s string) (string, []string) {
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
			if depth > 0 {
				depth--
			}
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

func trimPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
