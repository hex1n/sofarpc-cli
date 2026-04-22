package sourcecontract

import "strings"

// textutil.go holds the primitive byte/string helpers shared by the
// Java source parser. They are intentionally generic over "what's a
// Java identifier" vs "what's a modifier" so parse.go can stay focused
// on the grammar it cares about.

func findNextByte(s string, start int, want byte) int {
	for i := start; i < len(s); i++ {
		if s[i] == want {
			return i
		}
	}
	return -1
}

func findMatchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func findMatchingParen(s string, open int) int {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func findKeywordTopLevel(s, want string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		}
		if depth != 0 {
			continue
		}
		if strings.HasPrefix(s[i:], want) {
			beforeOK := i == 0 || !isIdentChar(s[i-1])
			after := i + len(want)
			afterOK := after >= len(s) || !isIdentChar(s[after])
			if beforeOK && afterOK {
				return i
			}
		}
	}
	return -1
}

func readClause(s, keyword string) (string, bool) {
	pos := findKeywordTopLevel(s, keyword)
	if pos < 0 {
		return "", false
	}
	rest := strings.TrimSpace(s[pos+len(keyword):])
	end := len(rest)
	for _, stop := range []string{"implements", "extends"} {
		if stop == keyword {
			continue
		}
		if idx := findKeywordTopLevel(rest, stop); idx >= 0 && idx < end {
			end = idx
		}
	}
	return strings.TrimSpace(rest[:end]), true
}

func splitTopLevel(s string, sep byte) []string {
	var out []string
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	depthAngle := 0
	depthParen := 0
	depthBrace := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depthAngle++
		case '>':
			if depthAngle > 0 {
				depthAngle--
			}
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '{':
			depthBrace++
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
		default:
			if s[i] == sep && depthAngle == 0 && depthParen == 0 && depthBrace == 0 {
				part := strings.TrimSpace(s[start:i])
				if part != "" {
					out = append(out, part)
				}
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

func splitWhitespace(s string) []string {
	var out []string
	depthAngle := 0
	start := -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depthAngle++
		case '>':
			if depthAngle > 0 {
				depthAngle--
			}
		}
		if isSpace(s[i]) && depthAngle == 0 {
			if start >= 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, strings.TrimSpace(s[start:]))
	}
	return out
}

func splitGeneric(s string) (string, []string) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '<')
	if open < 0 {
		return s, nil
	}
	base := strings.TrimSpace(s[:open])
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return base, splitTopLevel(s[open+1:i], ',')
			}
		}
	}
	return base, nil
}

func skipTypeParameters(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<") {
		return s
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return s[i+1:]
			}
		}
	}
	return s
}

func readIdentifier(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" || !isIdentStart(s[0]) {
		return "", s
	}
	i := 1
	for i < len(s) && isIdentChar(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func stripVariableName(token string) (string, string) {
	token = strings.TrimSpace(token)
	suffix := ""
	for strings.HasSuffix(token, "[]") {
		suffix += "[]"
		token = strings.TrimSpace(strings.TrimSuffix(token, "[]"))
	}
	if strings.HasPrefix(token, "...") {
		token = strings.TrimPrefix(token, "...")
		suffix += "[]"
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ""
	}
	if !isIdentStart(token[0]) {
		return "", ""
	}
	for i := 1; i < len(token); i++ {
		if !isIdentChar(token[i]) {
			return "", ""
		}
	}
	return token, suffix
}

func stripAnnotations(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			b.WriteByte(s[i])
			continue
		}
		i++
		for i < len(s) && (isIdentChar(s[i]) || s[i] == '.') {
			i++
		}
		if i < len(s) && s[i] == '(' {
			end := findMatchingParen(s, i)
			if end < 0 {
				return b.String()
			}
			i = end
		} else {
			i--
		}
	}
	return b.String()
}

func stripLeadingModifiers(s string) string {
	for {
		tokens := splitWhitespace(strings.TrimSpace(s))
		if len(tokens) == 0 || !isModifier(tokens[0]) {
			return strings.TrimSpace(s)
		}
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), tokens[0]))
	}
}

func hasModifier(s, want string) bool {
	for _, token := range splitWhitespace(stripAnnotations(s)) {
		if token == want {
			return true
		}
	}
	return false
}

func cutTopLevel(s string, sep byte) string {
	depthAngle := 0
	depthParen := 0
	depthBrace := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depthAngle++
		case '>':
			if depthAngle > 0 {
				depthAngle--
			}
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '{':
			depthBrace++
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
		default:
			if s[i] == sep && depthAngle == 0 && depthParen == 0 && depthBrace == 0 {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}

func startsNestedType(s string) bool {
	s = stripLeadingModifiers(strings.TrimSpace(stripAnnotations(s)))
	for _, prefix := range []string{"class ", "interface ", "enum ", "@interface "} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func isModifier(token string) bool {
	switch token {
	case "public", "protected", "private", "abstract", "default", "final", "static", "native", "synchronized", "transient", "volatile", "strictfp":
		return true
	default:
		return false
	}
}

func isPrimitive(s string) bool {
	switch s {
	case "boolean", "byte", "short", "int", "long", "float", "double", "char", "void":
		return true
	default:
		return false
	}
}

func isJavaLang(s string) bool {
	switch s {
	case "String", "Object", "Boolean", "Byte", "Short", "Integer", "Long", "Float", "Double", "Character", "Number", "Enum", "Throwable", "Exception", "RuntimeException", "Iterable", "Class", "Void":
		return true
	default:
		return false
	}
}

func isIdentStart(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '_' || b == '$'
}

func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
