package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadekit"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

var runJavap = defaultRunJavap
var resolveMethodFromArtifacts = ResolveMethodFromArtifacts

func DescribeServiceFromArtifacts(projectRoot, service string) (model.ServiceSchema, error) {
	loader, serviceInfo, err := loadArtifactService(projectRoot, service)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	return BuildServiceSchema(loader.registry, serviceInfo.FQN)
}

func ResolveMethodFromArtifacts(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (ProjectMethod, error) {
	loader, serviceInfo, err := loadArtifactService(projectRoot, service)
	if err != nil {
		return ProjectMethod{}, err
	}
	if serviceInfo.Kind != "interface" {
		return ProjectMethod{}, fmt.Errorf("service %s is not an interface", service)
	}
	methodInfo, err := selectSemanticMethod(serviceInfo, loader.registry, method, preferredParamTypes, rawArgs)
	if err != nil {
		return ProjectMethod{}, err
	}
	for _, parameter := range methodInfo.Parameters {
		loader.enqueueType(parameter.Type)
	}
	loader.enqueueType(methodInfo.ReturnType)
	if err := loader.expandQueue(); err != nil {
		return ProjectMethod{}, err
	}
	return ProjectMethod{
		ProjectRoot: projectRoot,
		Registry:    loader.registry,
		ServiceInfo: serviceInfo,
		MethodInfo:  methodInfo,
		Schema:      convertMethodSchema(serviceInfo, methodInfo, loader.registry),
	}, nil
}

func loadArtifactService(projectRoot, service string) (*javapLoader, facadekit.SemanticClassInfo, error) {
	layout, err := discoverProjectLayout(projectRoot)
	if err != nil {
		return nil, facadekit.SemanticClassInfo{}, err
	}
	match, err := matchServiceFn(layout.Root, service, layout.Modules)
	if err != nil {
		return nil, facadekit.SemanticClassInfo{}, err
	}
	artifacts, err := discoverArtifactsFn(layout.Root, match.Module)
	if err != nil {
		return nil, facadekit.SemanticClassInfo{}, err
	}
	classpath := append([]string{}, artifacts.PrimaryJars...)
	classpath = append(classpath, artifacts.DependencyJars...)
	loader := newJavapLoader(classpath)
	serviceInfo, err := loader.load(service, false)
	if err != nil {
		return nil, facadekit.SemanticClassInfo{}, err
	}
	return loader, serviceInfo, nil
}

type javapLoader struct {
	classpath []string
	registry  facadekit.Registry
	queued    map[string]struct{}
}

func newJavapLoader(classpath []string) *javapLoader {
	return &javapLoader{
		classpath: classpath,
		registry:  facadekit.Registry{},
		queued:    map[string]struct{}{},
	}
}

func (l *javapLoader) load(fqcn string, includeFields bool) (facadekit.SemanticClassInfo, error) {
	if info, ok := l.registry[fqcn]; ok && (includeFields == false || info.Kind == "interface" || len(info.Fields) > 0 || len(info.EnumConstants) > 0) {
		return info, nil
	}
	output, err := runJavap(l.classpath, includeFields, fqcn)
	if err != nil {
		return facadekit.SemanticClassInfo{}, err
	}
	info, err := parseJavapClass(output, fqcn)
	if err != nil {
		return facadekit.SemanticClassInfo{}, err
	}
	if cached, ok := l.registry[fqcn]; ok {
		if len(cached.Methods) > len(info.Methods) {
			info.Methods = cached.Methods
		}
		if cached.Kind != "" {
			info.Kind = cached.Kind
		}
	}
	l.registry[fqcn] = info
	return info, nil
}

func (l *javapLoader) enqueueType(typeStr string) {
	for _, candidate := range loadableTypes(typeStr) {
		if _, ok := l.registry[candidate]; ok {
			continue
		}
		l.queued[candidate] = struct{}{}
	}
}

func (l *javapLoader) expandQueue() error {
	for len(l.queued) > 0 {
		var target string
		for candidate := range l.queued {
			target = candidate
			break
		}
		delete(l.queued, target)
		info, err := l.load(target, true)
		if err != nil {
			continue
		}
		if info.Superclass != "" {
			l.enqueueType(info.Superclass)
		}
		for _, field := range info.Fields {
			l.enqueueType(field.JavaType)
		}
	}
	return nil
}

func defaultRunJavap(classpath []string, includeFields bool, fqcn string) (string, error) {
	args := []string{}
	if len(classpath) > 0 {
		args = append(args, "-classpath", strings.Join(classpath, string(os.PathListSeparator)))
	}
	if includeFields {
		args = append(args, "-private")
	} else {
		args = append(args, "-public")
	}
	args = append(args, fqcn)
	cmd := exec.Command("javap", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("javap %s failed: %w: %s", fqcn, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func parseJavapClass(output, fallbackFQCN string) (facadekit.SemanticClassInfo, error) {
	lines := strings.Split(output, "\n")
	var header string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Compiled from") {
			continue
		}
		if strings.HasSuffix(trimmed, "{") {
			header = trimmed
			break
		}
	}
	if header == "" {
		return facadekit.SemanticClassInfo{}, fmt.Errorf("javap output missing class header for %s", fallbackFQCN)
	}
	info, err := parseJavapHeader(header)
	if err != nil {
		return facadekit.SemanticClassInfo{}, err
	}
	if info.FQN == "" {
		info.FQN = fallbackFQCN
	}
	if idx := strings.LastIndex(info.FQN, "."); idx >= 0 {
		info.SamePkgPrefix = info.FQN[:idx]
		info.SimpleName = info.FQN[idx+1:]
	} else {
		info.SimpleName = info.FQN
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == header || trimmed == "}" || strings.HasPrefix(trimmed, "Compiled from") {
			continue
		}
		if !strings.HasSuffix(trimmed, ";") {
			continue
		}
		if strings.Contains(trimmed, "(") {
			method, ok := parseJavapMethod(trimmed, info.SimpleName)
			if ok {
				info.Methods = append(info.Methods, method)
			}
			continue
		}
		field, enumConstant, ok := parseJavapField(trimmed, info.Kind, info.FQN)
		if ok {
			if enumConstant != "" {
				info.EnumConstants = append(info.EnumConstants, enumConstant)
			} else {
				info.Fields = append(info.Fields, field)
			}
		}
	}
	return info, nil
}

func parseJavapHeader(header string) (facadekit.SemanticClassInfo, error) {
	clean := strings.TrimSuffix(strings.TrimSpace(header), "{")
	tokens := strings.Fields(clean)
	kindIndex := -1
	for idx, token := range tokens {
		switch token {
		case "class", "interface", "enum":
			kindIndex = idx
			goto found
		}
	}
found:
	if kindIndex == -1 || kindIndex+1 >= len(tokens) {
		return facadekit.SemanticClassInfo{}, fmt.Errorf("unable to parse javap header: %s", header)
	}
	kind := tokens[kindIndex]
	name := stripGenericSuffix(tokens[kindIndex+1])
	info := facadekit.SemanticClassInfo{
		FQN:  name,
		Kind: kind,
	}
	if kind == "enum" {
		info.Kind = "enum"
	}
	if kind == "class" {
		info.Kind = "class"
	}
	if kind == "interface" {
		info.Kind = "interface"
	}
	if idx := strings.Index(clean, " extends "); idx >= 0 && info.Kind == "class" {
		rest := clean[idx+len(" extends "):]
		if impl := strings.Index(rest, " implements "); impl >= 0 {
			rest = rest[:impl]
		}
		info.Superclass = stripGenericSuffix(strings.TrimSpace(rest))
	}
	return info, nil
}

func parseJavapField(line, kind, ownerFQCN string) (facadekit.SemanticFieldInfo, string, bool) {
	clean := strings.TrimSuffix(strings.TrimSpace(line), ";")
	if strings.Contains(" "+clean+" ", " static ") {
		if kind == "enum" {
			lastSpace := strings.LastIndex(clean, " ")
			if lastSpace <= 0 {
				return facadekit.SemanticFieldInfo{}, "", false
			}
			name := strings.TrimSpace(clean[lastSpace+1:])
			typeAndMods := strings.TrimSpace(clean[:lastSpace])
			fieldType := stripModifiers(typeAndMods)
			if fieldType == ownerFQCN {
				return facadekit.SemanticFieldInfo{}, name, true
			}
		}
		return facadekit.SemanticFieldInfo{}, "", false
	}
	lastSpace := strings.LastIndex(clean, " ")
	if lastSpace <= 0 {
		return facadekit.SemanticFieldInfo{}, "", false
	}
	name := strings.TrimSpace(clean[lastSpace+1:])
	typeAndMods := strings.TrimSpace(clean[:lastSpace])
	fieldType := stripModifiers(typeAndMods)
	if fieldType == "" {
		return facadekit.SemanticFieldInfo{}, "", false
	}
	if kind == "enum" && fieldType == ownerFQCN {
		return facadekit.SemanticFieldInfo{}, name, true
	}
	return facadekit.SemanticFieldInfo{Name: name, JavaType: fieldType}, "", true
}

func parseJavapMethod(line, simpleName string) (facadekit.SemanticMethodInfo, bool) {
	clean := strings.TrimSuffix(strings.TrimSpace(line), ";")
	if idx := strings.Index(clean, " throws "); idx >= 0 {
		clean = clean[:idx]
	}
	open := strings.Index(clean, "(")
	close := strings.LastIndex(clean, ")")
	if open <= 0 || close < open {
		return facadekit.SemanticMethodInfo{}, false
	}
	paramsPart := strings.TrimSpace(clean[open+1 : close])
	prefix := strings.TrimSpace(clean[:open])
	nameStart := strings.LastIndex(prefix, " ")
	if nameStart < 0 {
		return facadekit.SemanticMethodInfo{}, false
	}
	methodName := strings.TrimSpace(prefix[nameStart+1:])
	returnType := strings.TrimSpace(prefix[:nameStart])
	returnType = stripModifiers(returnType)
	returnType = stripMethodTypeParams(returnType)
	if returnType == "" || methodName == simpleName || strings.HasSuffix(methodName, "."+simpleName) {
		return facadekit.SemanticMethodInfo{}, false
	}
	params := splitTopLevelCSV(paramsPart)
	method := facadekit.SemanticMethodInfo{
		Name:       methodName,
		ReturnType: returnType,
		Parameters: make([]facadekit.SemanticParameterInfo, 0, len(params)),
	}
	for idx, param := range params {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}
		method.Parameters = append(method.Parameters, facadekit.SemanticParameterInfo{
			Name: fmt.Sprintf("arg%d", idx),
			Type: param,
		})
	}
	return method, true
}

func splitTopLevelCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	var current strings.Builder
	depth := 0
	for _, ch := range raw {
		switch ch {
		case '<':
			depth++
			current.WriteRune(ch)
		case '>':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			current.WriteRune(ch)
		default:
			current.WriteRune(ch)
		}
	}
	if token := strings.TrimSpace(current.String()); token != "" {
		out = append(out, token)
	}
	return out
}

func stripGenericSuffix(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if idx := strings.Index(typeName, "<"); idx >= 0 {
		return strings.TrimSpace(typeName[:idx])
	}
	return typeName
}

func stripModifiers(text string) string {
	for _, prefix := range []string{
		"public ", "protected ", "private ", "static ", "final ", "abstract ",
		"transient ", "volatile ", "synchronized ", "native ", "strictfp ",
	} {
		for strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return strings.TrimSpace(text)
}

func stripMethodTypeParams(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "<") {
		return text
	}
	depth := 0
	for idx, ch := range text {
		switch ch {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[idx+1:])
			}
		}
	}
	return text
}

func loadableTypes(typeStr string) []string {
	raw := strings.TrimSpace(typeStr)
	if raw == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	var visit func(string)
	visit = func(value string) {
		value = stripWildcard(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for strings.HasSuffix(value, "[]") {
			value = strings.TrimSpace(strings.TrimSuffix(value, "[]"))
		}
		head := stripGenericSuffix(value)
		if shouldLoadType(head) {
			if _, ok := seen[head]; !ok {
				seen[head] = struct{}{}
				out = append(out, head)
			}
		}
		for _, arg := range splitGenericArguments(value) {
			visit(arg)
		}
	}
	visit(raw)
	sort.Strings(out)
	return out
}

func shouldLoadType(typeName string) bool {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || isPrimitiveType(typeName) {
		return false
	}
	if strings.HasPrefix(typeName, "java.") || strings.HasPrefix(typeName, "javax.") || strings.HasPrefix(typeName, "sun.") {
		return false
	}
	if matchesNamedType(typeName, stringTypes) ||
		matchesNamedType(typeName, booleanTypes) ||
		matchesNamedType(typeName, numberTypes) ||
		matchesNamedType(typeName, decimalTypes) ||
		matchesNamedType(typeName, dateTypes) ||
		matchesNamedType(typeName, collectionTypes) ||
		matchesNamedType(typeName, mapTypes) ||
		isOptionalTypeName(typeName) {
		return false
	}
	return strings.Contains(typeName, ".")
}
