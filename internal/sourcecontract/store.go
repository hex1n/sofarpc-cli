// Package sourcecontract builds a contract.Store directly from Java source
// files under a project root. It exists so a fresh machine can run
// sofarpc_describe without Java, Spoon, or any local cache format.
package sourcecontract

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// Store is a lazily materialized contract store backed by Java source files.
// Load builds a light FQN->path index; Class(fqn) parses and caches a class on
// first access.
type Store struct {
	mu            sync.RWMutex
	index         map[string]string
	cache         map[string]javamodel.Class
	indexedFiles  int
	indexFailures map[string]string
	parseFailures map[string]string
}

type Diagnostics struct {
	IndexedClasses int               `json:"indexedClasses"`
	IndexedFiles   int               `json:"indexedFiles"`
	ParsedClasses  int               `json:"parsedClasses"`
	IndexFailures  map[string]string `json:"indexFailures,omitempty"`
	ParseFailures  map[string]string `json:"parseFailures,omitempty"`
}

// Load scans projectRoot for Java source files and returns a Store when at
// least one top-level type can be indexed. Hidden and build-output directories
// are skipped so editor worktrees and generated trees do not dominate the
// result set. The scan is intentionally shallow: it records only FQN->file
// mappings and defers field/method parsing until Class(fqn) is called.
func Load(projectRoot string) (*Store, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil, nil
	}
	info, err := os.Stat(projectRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	index, files, failures, err := scanProjectIndex(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(index) == 0 {
		return nil, nil
	}
	return &Store{
		index:         index,
		cache:         map[string]javamodel.Class{},
		indexedFiles:  files,
		indexFailures: failures,
		parseFailures: map[string]string{},
	}, nil
}

// Class implements contract.Store.
func (s *Store) Class(fqn string) (javamodel.Class, bool) {
	if s == nil {
		return javamodel.Class{}, false
	}
	s.mu.RLock()
	if c, ok := s.cache[fqn]; ok {
		s.mu.RUnlock()
		return c, true
	}
	path, ok := s.findIndexedPathFor(fqn)
	s.mu.RUnlock()
	if !ok {
		return javamodel.Class{}, false
	}

	classes, ok := parseJavaFileClasses(path)
	if !ok || len(classes) == 0 {
		s.recordParseFailure(fqn, "parseJavaFile returned no top-level declaration")
		return javamodel.Class{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, exists := s.cache[fqn]; exists {
		return cached, true
	}
	if s.cache == nil {
		s.cache = map[string]javamodel.Class{}
	}
	for _, cls := range classes {
		s.cache[cls.FQN] = cls
	}
	cls, exists := s.cache[fqn]
	if !exists {
		s.recordParseFailure(fqn, "parsed file did not materialize requested class")
		return javamodel.Class{}, false
	}
	return cls, true
}

// Size returns the number of classes loaded into the store.
func (s *Store) Size() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.index)
}

func (s *Store) Diagnostics() Diagnostics {
	if s == nil {
		return Diagnostics{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Diagnostics{
		IndexedClasses: len(s.index),
		IndexedFiles:   s.indexedFiles,
		ParsedClasses:  len(s.cache),
		IndexFailures:  cloneStringMap(s.indexFailures),
		ParseFailures:  cloneStringMap(s.parseFailures),
	}
}

func (s *Store) findIndexedPathFor(fqn string) (string, bool) {
	if s == nil {
		return "", false
	}
	if path, ok := s.index[fqn]; ok {
		return path, true
	}
	candidate := fqn
	for {
		cut := strings.LastIndexByte(candidate, '.')
		if cut < 0 {
			return "", false
		}
		candidate = candidate[:cut]
		if path, ok := s.index[candidate]; ok {
			return path, true
		}
	}
}

func (s *Store) recordParseFailure(fqn, detail string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parseFailures == nil {
		s.parseFailures = map[string]string{}
	}
	if _, exists := s.parseFailures[fqn]; exists {
		return
	}
	s.parseFailures[fqn] = detail
}

func scanProjectIndex(projectRoot string) (map[string]string, int, map[string]string, error) {
	index := map[string]string{}
	files := 0
	failures := map[string]string{}
	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(path, name) {
			return nil
		}
		files++
		fqn, ok := parseJavaIndexFile(path)
		if !ok || fqn == "" {
			if len(failures) < 8 {
				failures[path] = "could not identify top-level class/interface/enum"
			}
			return nil
		}
		if _, exists := index[fqn]; !exists {
			index[fqn] = path
		}
		return nil
	})
	if err != nil {
		return nil, 0, nil, err
	}
	return index, files, failures, nil
}

func parseJavaIndexFile(path string) (string, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	decl, ok := parseTopLevelType(clean)
	if !ok || decl.simpleName == "" {
		return "", false
	}
	if pkg == "" {
		return decl.simpleName, true
	}
	return pkg + "." + decl.simpleName, true
}

func parseJavaFileClasses(path string) ([]javamodel.Class, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	clean := sanitizeJava(string(body))
	pkg := parsePackage(clean)
	imports := parseImports(clean)
	decl, ok := parseTopLevelDecl(clean)
	if !ok || decl.simpleName == "" {
		return nil, false
	}
	return materializeClasses(path, pkg, imports, decl), true
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

func materializeClass(src parsedClass) javamodel.Class {
	cls := javamodel.Class{
		FQN:        src.fqn,
		SimpleName: src.simpleName,
		File:       src.path,
		Kind:       src.kind,
	}
	resolver := typeResolver{
		pkg:        src.packageName,
		explicit:   src.imports.explicit,
		wildcards:  src.imports.wildcards,
		localTypes: src.localTypes,
	}
	if src.superclass != "" {
		cls.Superclass = resolver.resolve(src.superclass)
	}
	for _, iface := range src.interfaces {
		cls.Interfaces = append(cls.Interfaces, resolver.resolve(iface))
	}
	for _, field := range src.fields {
		cls.Fields = append(cls.Fields, javamodel.Field{
			Name:     field.name,
			JavaType: resolver.resolve(field.javaType),
		})
	}
	for _, method := range src.methods {
		m := javamodel.Method{
			Name:       method.name,
			ReturnType: resolver.resolve(method.returnType),
		}
		for _, pt := range method.paramTypes {
			m.ParamTypes = append(m.ParamTypes, resolver.resolve(pt))
		}
		cls.Methods = append(cls.Methods, m)
	}
	cls.EnumConstants = append(cls.EnumConstants, src.enumConstants...)
	return cls
}

func materializeClasses(path, pkg string, imports importTable, decl declaration) []javamodel.Class {
	parsed := flattenDeclarations(path, pkg, imports, decl, nil, nil)
	if len(parsed) == 0 {
		return nil
	}
	out := make([]javamodel.Class, 0, len(parsed))
	for _, item := range parsed {
		out = append(out, materializeClass(item))
	}
	return out
}

func flattenDeclarations(path, pkg string, imports importTable, decl declaration, outers []string, visible map[string]string) []parsedClass {
	currentOuters := append(append([]string(nil), outers...), decl.simpleName)
	currentFQN := joinFQN(pkg, currentOuters)
	locals := cloneVisibleMap(visible)
	locals[decl.simpleName] = currentFQN
	for _, child := range decl.nested {
		locals[child.simpleName] = joinFQN(pkg, append(currentOuters, child.simpleName))
	}

	current := parsedClass{
		path:          path,
		packageName:   pkg,
		fqn:           currentFQN,
		simpleName:    decl.simpleName,
		kind:          decl.kind,
		superclass:    decl.superclass,
		interfaces:    append([]string(nil), decl.interfaces...),
		fields:        append([]parsedField(nil), decl.fields...),
		methods:       append([]parsedMethod(nil), decl.methods...),
		enumConstants: append([]string(nil), decl.enumConstants...),
		imports:       imports,
		localTypes:    locals,
	}

	out := []parsedClass{current}
	for _, child := range decl.nested {
		out = append(out, flattenDeclarations(path, pkg, imports, child, currentOuters, locals)...)
	}
	return out
}

func joinFQN(pkg string, names []string) string {
	name := strings.Join(names, ".")
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

func cloneVisibleMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

type parsedClass struct {
	path          string
	packageName   string
	fqn           string
	simpleName    string
	kind          string
	superclass    string
	interfaces    []string
	fields        []parsedField
	methods       []parsedMethod
	enumConstants []string
	imports       importTable
	localTypes    map[string]string
}

type parsedField struct {
	name     string
	javaType string
}

type parsedMethod struct {
	name       string
	paramTypes []string
	returnType string
}

type importTable struct {
	explicit  map[string]string
	wildcards []string
}

func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "target", "build", "out", "bin", "dist", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func shouldSkipFile(path, name string) bool {
	if filepath.Ext(name) != ".java" {
		return true
	}
	if name == "package-info.java" || name == "module-info.java" {
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(lower, "/src/test/") || strings.HasSuffix(name, "Test.java") {
		return true
	}
	return false
}

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
		kind:          decl.kind,
		superclass:    decl.superclass,
		interfaces:    decl.interfaces,
		fields:        decl.fields,
		methods:       decl.methods,
		enumConstants: decl.enumConstants,
		imports:       imports,
	}, true
}

type declaration struct {
	kind          string
	simpleName    string
	superclass    string
	interfaces    []string
	fields        []parsedField
	methods       []parsedMethod
	enumConstants []string
	nested        []declaration
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
	name := ""
	pos := findKeywordTopLevel(header, kind)
	if pos < 0 {
		return declaration{}
	}
	rest := strings.TrimSpace(header[pos+len(kind):])
	name, rest = readIdentifier(rest)
	if name == "" {
		return declaration{}
	}
	rest = strings.TrimSpace(skipTypeParameters(rest))
	decl := declaration{kind: kind, simpleName: name}

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

type typeResolver struct {
	pkg        string
	explicit   map[string]string
	wildcards  []string
	localTypes map[string]string
}

func (r typeResolver) resolve(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	if strings.HasPrefix(expr, "? extends ") {
		return "? extends " + r.resolve(strings.TrimSpace(strings.TrimPrefix(expr, "? extends ")))
	}
	if strings.HasPrefix(expr, "? super ") {
		return "? super " + r.resolve(strings.TrimSpace(strings.TrimPrefix(expr, "? super ")))
	}
	if expr == "?" {
		return expr
	}
	arraySuffix := ""
	for strings.HasSuffix(expr, "[]") {
		arraySuffix += "[]"
		expr = strings.TrimSpace(strings.TrimSuffix(expr, "[]"))
	}
	base, args := splitGeneric(expr)
	resolvedBase := r.resolveBase(base)
	if len(args) == 0 {
		return resolvedBase + arraySuffix
	}
	resolvedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		resolvedArgs = append(resolvedArgs, r.resolve(arg))
	}
	return resolvedBase + "<" + strings.Join(resolvedArgs, ", ") + ">" + arraySuffix
}

func (r typeResolver) resolveBase(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if isPrimitive(base) {
		return base
	}
	if fqn, ok := r.resolveLocalBase(base); ok {
		return fqn
	}
	if strings.Contains(base, ".") {
		return base
	}
	if fqn, ok := r.explicit[base]; ok {
		return fqn
	}
	if isJavaLang(base) {
		return "java.lang." + base
	}
	if r.pkg != "" {
		return r.pkg + "." + base
	}
	for _, prefix := range r.wildcards {
		if prefix != "" {
			return prefix + "." + base
		}
	}
	return base
}

func (r typeResolver) resolveLocalBase(base string) (string, bool) {
	if len(r.localTypes) == 0 {
		return "", false
	}
	if fqn, ok := r.localTypes[base]; ok {
		return fqn, true
	}
	head, tail, ok := strings.Cut(base, ".")
	if !ok {
		return "", false
	}
	if fqn, ok := r.localTypes[head]; ok {
		return fqn + "." + tail, true
	}
	if fqn, ok := r.explicit[head]; ok {
		return fqn + "." + tail, true
	}
	if isJavaLang(head) {
		return "java.lang." + head + "." + tail, true
	}
	return "", false
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
