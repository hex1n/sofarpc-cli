package sourcecontract

import (
	"os"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// declaration is the raw, pre-FQN view of one Java type declaration. It
// carries children in nested form so flattenDeclarations can walk the
// outer→inner chain when computing FQNs.
type declaration struct {
	kind          string
	simpleName    string
	typeParams    map[string]string
	superclass    string
	interfaces    []string
	fields        []parsedField
	methods       []parsedMethod
	enumConstants []string
	nested        []declaration
}

// parseJavaFile returns the top-level declaration of path as a
// parsedClass (no nested flattening). It is retained for callers that
// need the pre-flatten shape; parseJavaFileClasses is preferred for
// producing javamodel.Class values.
func parseJavaFile(path string) (parsedClass, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return parsedClass{}, false
	}
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	imports := parseImports(clean)
	decl, ok := parseTopLevelDecl(clean)
	if !ok {
		return parsedClass{}, false
	}
	fqn := decl.simpleName
	if pkg != "" {
		fqn = pkg + "." + decl.simpleName
	}
	return parsedClass{
		path:          path,
		packageName:   pkg,
		fqn:           fqn,
		simpleName:    decl.simpleName,
		typeParams:    cloneStringMap(decl.typeParams),
		kind:          decl.kind,
		superclass:    decl.superclass,
		interfaces:    decl.interfaces,
		fields:        decl.fields,
		methods:       decl.methods,
		enumConstants: decl.enumConstants,
		imports:       imports,
	}, true
}

func parseTopLevelType(src string) (declaration, bool) {
	for i := 0; i < len(src); i++ {
		kind, ok := matchTopLevelKind(src, i)
		if !ok {
			continue
		}
		open := findNextByte(src, i, '{')
		if open < 0 {
			return declaration{}, false
		}
		header := strings.TrimSpace(src[i:open])
		decl := parseDeclarationHeader(kind, header)
		if decl.simpleName == "" {
			return declaration{}, false
		}
		return decl, true
	}
	return declaration{}, false
}

func parseTopLevelDecl(src string) (declaration, bool) {
	return parseDeclarationSegment(src)
}

func parseDeclarationSegment(src string) (declaration, bool) {
	for i := 0; i < len(src); i++ {
		kind, ok := matchTopLevelKind(src, i)
		if !ok {
			continue
		}
		open := findNextByte(src, i, '{')
		if open < 0 {
			return declaration{}, false
		}
		close := findMatchingBrace(src, open)
		if close < 0 {
			return declaration{}, false
		}
		header := strings.TrimSpace(src[i:open])
		decl := parseDeclarationHeader(kind, header)
		if decl.simpleName == "" {
			return declaration{}, false
		}
		body := src[open+1 : close]
		decl.enumConstants = parseEnumConstants(decl.kind, body)
		for _, segment := range splitMembers(body) {
			if nested, ok := parseNestedDeclaration(segment); ok {
				decl.nested = append(decl.nested, nested)
				continue
			}
			if field, ok := parseFieldSegment(segment); ok {
				decl.fields = append(decl.fields, field)
			}
			if method, ok := parseMethodSegment(segment, decl.simpleName); ok {
				decl.methods = append(decl.methods, method)
			}
		}
		return decl, true
	}
	return declaration{}, false
}

func parseNestedDeclaration(segment string) (declaration, bool) {
	trimmed := strings.TrimSpace(segment)
	if trimmed == "" || !startsNestedType(trimmed) {
		return declaration{}, false
	}
	return parseDeclarationSegment(trimmed)
}

func matchTopLevelKind(src string, idx int) (string, bool) {
	if idx > 0 {
		prev := src[idx-1]
		if isIdentChar(prev) || prev == '@' {
			return "", false
		}
	}
	for _, kind := range []string{"class", "interface", "enum"} {
		if !strings.HasPrefix(src[idx:], kind) {
			continue
		}
		end := idx + len(kind)
		if end < len(src) && isIdentChar(src[end]) {
			continue
		}
		return kind, true
	}
	return "", false
}

func parseDeclarationHeader(kind, header string) declaration {
	pos := findKeywordTopLevel(header, kind)
	if pos < 0 {
		return declaration{}
	}
	rest := strings.TrimSpace(header[pos+len(kind):])
	name, rest := readIdentifier(rest)
	if name == "" {
		return declaration{}
	}
	typeParams, rest := readTypeParameterBounds(rest)
	decl := declaration{kind: kind, simpleName: name, typeParams: typeParams}

	if kind == javamodel.KindClass {
		if clause, ok := readClause(rest, "extends"); ok {
			decl.superclass = clause
		}
		if clause, ok := readClause(rest, "implements"); ok {
			decl.interfaces = splitTopLevel(clause, ',')
		}
		return decl
	}
	if kind == javamodel.KindInterface {
		if clause, ok := readClause(rest, "extends"); ok {
			decl.interfaces = splitTopLevel(clause, ',')
		}
		return decl
	}
	if clause, ok := readClause(rest, "implements"); ok {
		decl.interfaces = splitTopLevel(clause, ',')
	}
	return decl
}

func parseEnumConstants(kind, body string) []string {
	if kind != javamodel.KindEnum {
		return nil
	}
	head := body
	depth := 0
	parens := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			parens++
		case ')':
			if parens > 0 {
				parens--
			}
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 && parens == 0 {
				head = body[:i]
				i = len(body)
			}
		}
	}
	var out []string
	for _, part := range splitTopLevel(head, ',') {
		part = stripAnnotations(part)
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _ := readIdentifier(part)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func splitMembers(body string) []string {
	var out []string
	start := 0
	depth := 0
	parens := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			parens++
		case ')':
			if parens > 0 {
				parens--
			}
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
			if depth == 0 && parens == 0 {
				segment := strings.TrimSpace(body[start : i+1])
				if segment != "" {
					out = append(out, segment)
				}
				start = i + 1
			}
		case ';':
			if depth == 0 && parens == 0 {
				segment := strings.TrimSpace(body[start : i+1])
				if segment != "" {
					out = append(out, segment)
				}
				start = i + 1
			}
		}
	}
	return out
}

func parseFieldSegment(segment string) (parsedField, bool) {
	trimmed := strings.TrimSpace(segment)
	if trimmed == "" {
		return parsedField{}, false
	}
	trimmed = stripAnnotations(trimmed)
	if strings.Contains(trimmed, "(") {
		return parsedField{}, false
	}
	if startsNestedType(trimmed) || strings.HasPrefix(trimmed, "static {") {
		return parsedField{}, false
	}
	trimmed = strings.TrimSuffix(trimmed, ";")
	isStatic := hasModifier(trimmed, "static")
	trimmed = stripLeadingModifiers(trimmed)
	trimmed = cutTopLevel(trimmed, '=')
	tokens := splitWhitespace(trimmed)
	if len(tokens) < 2 || isStatic {
		return parsedField{}, false
	}
	nameToken := tokens[len(tokens)-1]
	typeExpr := strings.Join(tokens[:len(tokens)-1], " ")
	name, suffix := stripVariableName(nameToken)
	if name == "" {
		return parsedField{}, false
	}
	return parsedField{
		name:     name,
		javaType: strings.TrimSpace(typeExpr + suffix),
	}, true
}

func parseMethodSegment(segment, simpleName string) (parsedMethod, bool) {
	trimmed := strings.TrimSpace(segment)
	if trimmed == "" || startsNestedType(trimmed) || !strings.Contains(trimmed, "(") {
		return parsedMethod{}, false
	}
	trimmed = stripAnnotations(trimmed)
	open := strings.IndexByte(trimmed, '(')
	if open < 0 {
		return parsedMethod{}, false
	}
	close := findMatchingParen(trimmed, open)
	if close < 0 {
		return parsedMethod{}, false
	}
	prefix := strings.TrimSpace(trimmed[:open])
	prefix = stripLeadingModifiers(prefix)
	prefix = strings.TrimSpace(skipTypeParameters(prefix))
	tokens := splitWhitespace(prefix)
	if len(tokens) < 2 {
		return parsedMethod{}, false
	}
	name := tokens[len(tokens)-1]
	returnType := tokens[len(tokens)-2]
	if name == simpleName || isModifier(returnType) {
		return parsedMethod{}, false
	}
	params := parseParams(trimmed[open+1 : close])
	return parsedMethod{
		name:       name,
		paramTypes: params,
		returnType: returnType,
	}, true
}

func parseParams(raw string) []string {
	var out []string
	for _, part := range splitTopLevel(raw, ',') {
		part = strings.TrimSpace(stripAnnotations(part))
		if part == "" {
			continue
		}
		part = stripLeadingModifiers(part)
		tokens := splitWhitespace(part)
		if len(tokens) < 2 {
			continue
		}
		name, suffix := stripVariableName(tokens[len(tokens)-1])
		if name == "" {
			continue
		}
		typeExpr := strings.Join(tokens[:len(tokens)-1], " ")
		typeExpr = strings.TrimSpace(typeExpr + suffix)
		typeExpr = strings.ReplaceAll(typeExpr, "...", "[]")
		out = append(out, typeExpr)
	}
	return out
}

func parsePackage(src string) string {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package ") {
			continue
		}
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "package "), ";"))
		return line
	}
	return ""
}

func parseImports(src string) importTable {
	out := importTable{explicit: map[string]string{}}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "import static ") {
			continue
		}
		target := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "import "), ";"))
		if strings.HasSuffix(target, ".*") {
			out.wildcards = append(out.wildcards, strings.TrimSuffix(target, ".*"))
			continue
		}
		simple := target
		if idx := strings.LastIndexByte(target, '.'); idx >= 0 {
			simple = target[idx+1:]
		}
		out.explicit[simple] = target
	}
	return out
}

// sanitizeJava blanks comments and string/char literals while preserving
// newline positions so downstream slicing keeps line-based parsing sane.
func sanitizeJava(src string) string {
	buf := make([]byte, len(src))
	copy(buf, src)
	const (
		stateNormal = iota
		stateLineComment
		stateBlockComment
		stateSingleQuote
		stateDoubleQuote
	)
	state := stateNormal
	for i := 0; i < len(buf); i++ {
		switch state {
		case stateNormal:
			if i+1 < len(buf) && buf[i] == '/' && buf[i+1] == '/' {
				buf[i], buf[i+1] = ' ', ' '
				state = stateLineComment
				i++
				continue
			}
			if i+1 < len(buf) && buf[i] == '/' && buf[i+1] == '*' {
				buf[i], buf[i+1] = ' ', ' '
				state = stateBlockComment
				i++
				continue
			}
			if buf[i] == '\'' {
				buf[i] = ' '
				state = stateSingleQuote
				continue
			}
			if buf[i] == '"' {
				buf[i] = ' '
				state = stateDoubleQuote
			}
		case stateLineComment:
			if buf[i] == '\n' {
				state = stateNormal
				continue
			}
			buf[i] = ' '
		case stateBlockComment:
			if i+1 < len(buf) && buf[i] == '*' && buf[i+1] == '/' {
				buf[i], buf[i+1] = ' ', ' '
				state = stateNormal
				i++
				continue
			}
			if buf[i] != '\n' && buf[i] != '\r' {
				buf[i] = ' '
			}
		case stateSingleQuote:
			if buf[i] == '\\' && i+1 < len(buf) {
				buf[i], buf[i+1] = ' ', ' '
				i++
				continue
			}
			if buf[i] == '\'' {
				buf[i] = ' '
				state = stateNormal
				continue
			}
			if buf[i] != '\n' && buf[i] != '\r' {
				buf[i] = ' '
			}
		case stateDoubleQuote:
			if buf[i] == '\\' && i+1 < len(buf) {
				buf[i], buf[i+1] = ' ', ' '
				i++
				continue
			}
			if buf[i] == '"' {
				buf[i] = ' '
				state = stateNormal
				continue
			}
			if buf[i] != '\n' && buf[i] != '\r' {
				buf[i] = ' '
			}
		}
	}
	return string(buf)
}
