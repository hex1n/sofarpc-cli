package facadekit

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	sofaLiPattern   = regexp.MustCompile(`<li>\s*([A-Za-z_]\w*)\s*\|\s*([^|<]+?)\s*\|`)
	paramDocPattern = regexp.MustCompile(`@param\s+([A-Za-z_]\w*)\s+([^@]+)`)
)

var (
	primitiveZero = map[string]struct{}{
		"byte": {}, "short": {}, "int": {}, "long": {}, "float": {}, "double": {},
		"Byte": {}, "Short": {}, "Integer": {}, "Long": {}, "Float": {}, "Double": {},
		"Number": {}, "AtomicInteger": {}, "AtomicLong": {},
	}
	stringLike = map[string]struct{}{
		"String": {}, "CharSequence": {}, "UUID": {},
	}
	boolLike = map[string]struct{}{
		"boolean": {}, "Boolean": {},
	}
	decimalLike = map[string]struct{}{
		"BigDecimal": {}, "BigInteger": {},
	}
	dateLike = map[string]struct{}{
		"Date": {}, "LocalDate": {}, "LocalDateTime": {}, "Instant": {}, "Timestamp": {},
		"LocalTime": {}, "OffsetDateTime": {}, "ZonedDateTime": {},
	}
	collectionLike = map[string]struct{}{
		"List": {}, "ArrayList": {}, "LinkedList": {}, "Collection": {}, "Iterable": {},
		"Set": {}, "HashSet": {}, "LinkedHashSet": {}, "TreeSet": {},
	}
	mapLike = map[string]struct{}{
		"Map": {}, "HashMap": {}, "LinkedHashMap": {}, "TreeMap": {}, "ConcurrentHashMap": {},
	}
)

type TypeInfo struct {
	Raw      string      `json:"raw"`
	Category string      `json:"category"`
	FQN      string      `json:"fqn,omitempty"`
	Values   []string    `json:"values,omitempty"`
	Element  *TypeInfo   `json:"element,omitempty"`
	Key      *TypeInfo   `json:"key,omitempty"`
	Value    *TypeInfo   `json:"value,omitempty"`
	Hint     interface{} `json:"hint,omitempty"`
}

type FieldSchema struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Comment  string   `json:"comment,omitempty"`
	TypeInfo TypeInfo `json:"typeInfo"`
}

type ParameterSchema struct {
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	TypeInfo     TypeInfo      `json:"typeInfo"`
	Fields       []FieldSchema `json:"fields,omitempty"`
	RequiredHint string        `json:"requiredHint,omitempty"`
}

type MethodSchemaResult struct {
	Name                  string            `json:"name"`
	Javadoc               string            `json:"javadoc,omitempty"`
	ReturnType            string            `json:"returnType,omitempty"`
	ParamTypes            []string          `json:"paramTypes"`
	ParamsSkeleton        []interface{}     `json:"paramsSkeleton"`
	ParamsFieldInfo       []ParameterSchema `json:"paramsFieldInfo"`
	ResponseWarning       string            `json:"responseWarning,omitempty"`
	ResponseWarningReason string            `json:"responseWarningReason,omitempty"`
}

type MethodSchemaEnvelope struct {
	Service string             `json:"service"`
	File    string             `json:"file,omitempty"`
	Method  MethodSchemaResult `json:"method"`
}

type buildContext struct {
	registry Registry
	visited  map[string]struct{}
}

func BuildMethodSchema(registry Registry, service, method string, preferredParamTypes []string, markers []string) (MethodSchemaEnvelope, error) {
	serviceInfo, ok := registry[service]
	if !ok {
		return MethodSchemaEnvelope{}, fmt.Errorf("service %s not found in semantic registry", service)
	}
	if serviceInfo.Kind != "interface" {
		return MethodSchemaEnvelope{}, fmt.Errorf("service %s is not an interface", service)
	}
	methodInfo, err := selectMethod(serviceInfo.Methods, method, preferredParamTypes)
	if err != nil {
		return MethodSchemaEnvelope{}, err
	}
	result := buildMethodSchemaResult(registry, serviceInfo, methodInfo, markers)
	return MethodSchemaEnvelope{
		Service: serviceInfo.FQN,
		File:    serviceInfo.File,
		Method:  result,
	}, nil
}

func buildMethodSchemaResult(registry Registry, serviceInfo SemanticClassInfo, methodInfo SemanticMethodInfo, markers []string) MethodSchemaResult {
	ctx := buildContext{
		registry: registry,
		visited:  map[string]struct{}{},
	}
	requiredHints := extractRequiredHintsFromJavadoc(methodInfo.Javadoc, markers)
	paramsSkeleton := make([]interface{}, 0, len(methodInfo.Parameters))
	paramsFieldInfo := make([]ParameterSchema, 0, len(methodInfo.Parameters))
	for _, param := range methodInfo.Parameters {
		value, meta := skeletonForType(param.Type, serviceInfo, ctx)
		entry := ParameterSchema{
			Name:     param.Name,
			Type:     param.Type,
			TypeInfo: meta,
		}
		if meta.Category == "object" && meta.FQN != "" {
			if target, ok := registry[meta.FQN]; ok {
				entry.Fields = fieldInfoForClass(target, ctx)
				applyJavadocRequired(entry.Fields, requiredHints)
			}
		}
		if hint, ok := requiredHints[param.Name]; ok {
			entry.RequiredHint = hint
		}
		paramsSkeleton = append(paramsSkeleton, value)
		paramsFieldInfo = append(paramsFieldInfo, entry)
	}
	result := MethodSchemaResult{
		Name:            methodInfo.Name,
		Javadoc:         methodInfo.Javadoc,
		ReturnType:      methodInfo.ReturnType,
		ParamTypes:      collectParamTypes(methodInfo),
		ParamsSkeleton:  paramsSkeleton,
		ParamsFieldInfo: paramsFieldInfo,
	}
	if reason := detectEnvelopeOptionalWarning(methodInfo.ReturnType, serviceInfo, registry); reason != "" {
		result.ResponseWarning = "response wrapper exposes Optional/helper getters; prefer raw mode when stub jars are complete, generic mode may lose nested DTO types"
		result.ResponseWarningReason = reason
	}
	return result
}

func selectMethod(methods []SemanticMethodInfo, name string, preferredParamTypes []string) (SemanticMethodInfo, error) {
	var matches []SemanticMethodInfo
	for _, method := range methods {
		if method.Name == name {
			matches = append(matches, method)
		}
	}
	if len(matches) == 0 {
		return SemanticMethodInfo{}, fmt.Errorf("method %s not found", name)
	}
	if len(preferredParamTypes) > 0 {
		var narrowed []SemanticMethodInfo
		for _, candidate := range matches {
			if sameParamTypes(collectParamTypes(candidate), preferredParamTypes) {
				narrowed = append(narrowed, candidate)
			}
		}
		if len(narrowed) == 1 {
			return narrowed[0], nil
		}
		if len(narrowed) > 1 {
			matches = narrowed
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	options := make([]string, 0, len(matches))
	for _, candidate := range matches {
		options = append(options, "["+strings.Join(collectParamTypes(candidate), ",")+"]")
	}
	sort.Strings(options)
	return SemanticMethodInfo{}, fmt.Errorf("method %s is overloaded; pass --types to disambiguate: %s", name, strings.Join(options, " "))
}

func collectParamTypes(method SemanticMethodInfo) []string {
	out := make([]string, 0, len(method.Parameters))
	for _, parameter := range method.Parameters {
		out = append(out, typeHead(parameter.Type))
	}
	return out
}

func sameParamTypes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if typeHead(left[i]) != typeHead(right[i]) {
			return false
		}
	}
	return true
}

func typeHead(typeStr string) string {
	return strings.TrimSpace(strings.Split(strings.Split(typeStr, "<")[0], "[")[0])
}

func stripWildcard(typeStr string) string {
	text := strings.TrimSpace(typeStr)
	for _, prefix := range []string{"? extends ", "? super "} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	if text == "?" {
		return "Object"
	}
	return text
}

func simpleTypeName(typeStr string) string {
	text := stripWildcard(typeStr)
	if idx := strings.LastIndex(text, "."); idx >= 0 {
		return text[idx+1:]
	}
	return text
}

func matchesType(typeStr string, known map[string]struct{}) bool {
	head := typeHead(typeStr)
	_, ok := known[head]
	if ok {
		return true
	}
	_, ok = known[simpleTypeName(head)]
	return ok
}

func resolveFQN(typeStr string, owner SemanticClassInfo, registry Registry) string {
	head := stripWildcard(typeHead(typeStr))
	simple := simpleTypeName(head)
	if _, ok := registry[head]; ok {
		return head
	}
	if owner.Imports != nil {
		if candidate, ok := owner.Imports[simple]; ok {
			if _, present := registry[candidate]; present {
				return candidate
			}
		}
	}
	if owner.SamePkgPrefix != "" {
		candidate := owner.SamePkgPrefix + "." + simple
		if _, ok := registry[candidate]; ok {
			return candidate
		}
	}
	var suffixMatches []string
	for fqn := range registry {
		if fqn == head || strings.HasSuffix(fqn, "."+simple) {
			suffixMatches = append(suffixMatches, fqn)
		}
	}
	if len(suffixMatches) == 1 {
		return suffixMatches[0]
	}
	return ""
}

func parseGenericArgs(typeStr string) []string {
	lt := strings.Index(typeStr, "<")
	if lt == -1 {
		return nil
	}
	depth := 0
	var buf strings.Builder
	var out []string
	for _, ch := range typeStr[lt:] {
		switch ch {
		case '<':
			depth++
			if depth > 1 {
				buf.WriteRune(ch)
			}
		case '>':
			depth--
			if depth == 0 {
				token := strings.TrimSpace(buf.String())
				if token != "" {
					out = append(out, token)
				}
				return out
			}
			buf.WriteRune(ch)
		case ',':
			if depth == 1 {
				token := strings.TrimSpace(buf.String())
				if token != "" {
					out = append(out, token)
				}
				buf.Reset()
				continue
			}
			buf.WriteRune(ch)
		default:
			buf.WriteRune(ch)
		}
	}
	return out
}

func skeletonForType(typeStr string, owner SemanticClassInfo, ctx buildContext) (interface{}, TypeInfo) {
	head := stripWildcard(typeHead(typeStr))
	simpleHead := simpleTypeName(head)
	arrayDims := strings.Count(typeStr, "[]")
	meta := TypeInfo{Raw: typeStr}

	if arrayDims > 0 {
		inner := strings.TrimSpace(typeStr[:strings.Index(typeStr, "[")])
		innerValue, innerMeta := skeletonForType(inner, owner, ctx)
		meta.Category = "array"
		meta.Element = &innerMeta
		return []interface{}{innerValue}, meta
	}

	switch {
	case matchesType(head, stringLike):
		meta.Category = "string"
		return "", meta
	case matchesType(head, boolLike):
		meta.Category = "boolean"
		return false, meta
	case matchesType(head, primitiveZero):
		meta.Category = "number"
		return 0, meta
	case matchesType(head, decimalLike):
		meta.Category = "decimal"
		meta.Hint = `pass as string for hessian2 safety, e.g. "0"`
		return "0", meta
	case matchesType(head, dateLike):
		meta.Category = "date"
		switch simpleHead {
		case "Date":
			meta.Hint = "yyyy-MM-dd HH:mm:ss"
		case "LocalDate":
			meta.Hint = "yyyy-MM-dd"
		case "LocalDateTime":
			meta.Hint = "yyyy-MM-dd'T'HH:mm:ss"
		default:
			meta.Hint = "ISO-8601"
		}
		return nil, meta
	case matchesType(head, map[string]struct{}{"Object": {}}):
		meta.Category = "unknown"
		return nil, meta
	}

	args := parseGenericArgs(typeStr)
	if isOptionalType(head) {
		meta.Category = "optional"
		if len(args) > 0 {
			innerValue, innerMeta := skeletonForType(args[0], owner, ctx)
			meta.Value = &innerMeta
			return innerValue, meta
		}
		return nil, meta
	}
	if matchesType(head, collectionLike) {
		meta.Category = "collection"
		if len(args) > 0 {
			innerValue, innerMeta := skeletonForType(args[0], owner, ctx)
			meta.Element = &innerMeta
			return []interface{}{innerValue}, meta
		}
		return []interface{}{}, meta
	}
	if matchesType(head, mapLike) {
		meta.Category = "map"
		if len(args) == 2 {
			_, keyMeta := skeletonForType(args[0], owner, ctx)
			valueValue, valueMeta := skeletonForType(args[1], owner, ctx)
			meta.Key = &keyMeta
			meta.Value = &valueMeta
			return map[string]interface{}{"": valueValue}, meta
		}
		return map[string]interface{}{}, meta
	}

	fqn := resolveFQN(head, owner, ctx.registry)
	if fqn != "" {
		if target, ok := ctx.registry[fqn]; ok {
			switch target.Kind {
			case "enum":
				meta.Category = "enum"
				meta.FQN = fqn
				meta.Values = append([]string{}, target.EnumConstants...)
				if len(target.EnumConstants) > 0 {
					return target.EnumConstants[0], meta
				}
				return "", meta
			case "class":
				meta.Category = "object"
				meta.FQN = fqn
				return skeletonForClass(target, ctx), meta
			}
		}
	}

	meta.Category = "unresolved"
	if fqn != "" {
		meta.FQN = fqn
	}
	return map[string]interface{}{}, meta
}

func collectFields(classInfo SemanticClassInfo, ctx buildContext) []SemanticFieldInfo {
	seen := map[string]struct{}{}
	var out []SemanticFieldInfo
	var chain []SemanticClassInfo
	current := classInfo
	for i := 0; i < 20; i++ {
		chain = append(chain, current)
		if strings.TrimSpace(current.Superclass) == "" {
			break
		}
		parentFQN := resolveFQN(current.Superclass, current, ctx.registry)
		parent, ok := ctx.registry[parentFQN]
		if !ok {
			break
		}
		current = parent
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, field := range chain[i].Fields {
			if _, ok := seen[field.Name]; ok {
				continue
			}
			seen[field.Name] = struct{}{}
			out = append(out, field)
		}
	}
	return out
}

func skeletonForClass(classInfo SemanticClassInfo, ctx buildContext) map[string]interface{} {
	if _, ok := ctx.visited[classInfo.FQN]; ok {
		return map[string]interface{}{"$circular": classInfo.FQN}
	}
	ctx.visited[classInfo.FQN] = struct{}{}
	defer delete(ctx.visited, classInfo.FQN)

	result := map[string]interface{}{}
	for _, field := range collectFields(classInfo, ctx) {
		value, _ := skeletonForType(field.JavaType, classInfo, ctx)
		result[field.Name] = value
	}
	return result
}

func fieldInfoForClass(classInfo SemanticClassInfo, ctx buildContext) []FieldSchema {
	fields := collectFields(classInfo, ctx)
	out := make([]FieldSchema, 0, len(fields))
	for _, field := range fields {
		_, meta := skeletonForType(field.JavaType, classInfo, ctx)
		out = append(out, FieldSchema{
			Name:     field.Name,
			Type:     field.JavaType,
			Required: field.Required,
			Comment:  field.Comment,
			TypeInfo: meta,
		})
	}
	return out
}

func extractRequiredHintsFromJavadoc(doc string, markers []string) map[string]string {
	hints := map[string]string{}
	for _, match := range sofaLiPattern.FindAllStringSubmatch(doc, -1) {
		if len(match) < 3 {
			continue
		}
		name := match[1]
		tag := match[2]
		if containsRequiredMarker(tag, markers) {
			hints[name] = match[0]
		}
	}
	for _, match := range paramDocPattern.FindAllStringSubmatch(doc, -1) {
		if len(match) < 3 {
			continue
		}
		name := match[1]
		body := strings.TrimSpace(match[2])
		if containsRequiredMarker(body, markers) {
			if _, exists := hints[name]; !exists {
				hints[name] = body
			}
		}
	}
	return hints
}

func containsRequiredMarker(text string, markers []string) bool {
	lower := strings.ToLower(text)
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker != "" && strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func applyJavadocRequired(fields []FieldSchema, requiredHints map[string]string) {
	for i := range fields {
		if _, ok := requiredHints[fields[i].Name]; ok {
			fields[i].Required = true
		}
	}
}

func isOptionalType(typeStr string) bool {
	return matchesType(typeStr, map[string]struct{}{"Optional": {}})
}

func detectEnvelopeOptionalWarning(returnType string, owner SemanticClassInfo, registry Registry) string {
	head := typeHead(returnType)
	if head == "" || head == "void" || head == "Object" {
		return ""
	}
	fqn := resolveFQN(head, owner, registry)
	if fqn == "" {
		return ""
	}
	classInfo, ok := registry[fqn]
	if !ok || classInfo.Kind != "class" {
		return ""
	}
	for _, field := range classInfo.Fields {
		if isOptionalType(field.JavaType) {
			return fmt.Sprintf("%s.%s: %s", classInfo.SimpleName, field.Name, field.JavaType)
		}
	}
	for _, returnType := range classInfo.MethodReturns {
		if isOptionalType(returnType) {
			return fmt.Sprintf("%s exposes Optional getter (%s)", classInfo.SimpleName, returnType)
		}
	}
	return ""
}
