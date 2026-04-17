package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

var (
	stringTypes = map[string]struct{}{
		"java.lang.String": {}, "java.lang.CharSequence": {}, "java.util.UUID": {},
		"String": {}, "CharSequence": {}, "UUID": {},
	}
	booleanTypes = map[string]struct{}{
		"boolean": {}, "java.lang.Boolean": {}, "Boolean": {},
	}
	numberTypes = map[string]struct{}{
		"byte": {}, "short": {}, "int": {}, "long": {}, "float": {}, "double": {},
		"java.lang.Byte": {}, "java.lang.Short": {}, "java.lang.Integer": {}, "java.lang.Long": {},
		"java.lang.Float": {}, "java.lang.Double": {}, "java.lang.Number": {},
		"Byte": {}, "Short": {}, "Integer": {}, "Long": {}, "Float": {}, "Double": {}, "Number": {},
	}
	decimalTypes = map[string]struct{}{
		"java.math.BigDecimal": {}, "java.math.BigInteger": {},
		"BigDecimal": {}, "BigInteger": {},
	}
	dateTypes = map[string]struct{}{
		"java.util.Date": {}, "java.sql.Timestamp": {}, "java.time.LocalDate": {}, "java.time.LocalDateTime": {},
		"java.time.Instant": {}, "java.time.LocalTime": {}, "java.time.OffsetDateTime": {}, "java.time.ZonedDateTime": {},
		"Date": {}, "Timestamp": {}, "LocalDate": {}, "LocalDateTime": {}, "Instant": {}, "LocalTime": {}, "OffsetDateTime": {}, "ZonedDateTime": {},
	}
	collectionTypes = map[string]struct{}{
		"java.util.List": {}, "java.util.ArrayList": {}, "java.util.LinkedList": {}, "java.util.Collection": {}, "java.lang.Iterable": {},
		"java.util.Set": {}, "java.util.HashSet": {}, "java.util.LinkedHashSet": {}, "java.util.TreeSet": {},
		"List": {}, "ArrayList": {}, "LinkedList": {}, "Collection": {}, "Iterable": {}, "Set": {}, "HashSet": {}, "LinkedHashSet": {}, "TreeSet": {},
	}
	mapTypes = map[string]struct{}{
		"java.util.Map": {}, "java.util.HashMap": {}, "java.util.LinkedHashMap": {}, "java.util.TreeMap": {}, "java.util.concurrent.ConcurrentHashMap": {},
		"Map": {}, "HashMap": {}, "LinkedHashMap": {}, "TreeMap": {}, "ConcurrentHashMap": {},
	}
)

type javaType struct {
	Category string
	Raw      string
	FQN      string
	Element  *javaType
	Key      *javaType
	Value    *javaType
}

func CompileProjectMethodArgs(raw json.RawMessage, method ProjectMethod) (json.RawMessage, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	items, ok := decoded.([]any)
	if !ok {
		return nil, fmt.Errorf("args must be a JSON array before generic compilation")
	}
	compiled := make([]any, len(items))
	for idx, item := range items {
		if idx >= len(method.MethodInfo.Parameters) {
			compiled[idx] = item
			continue
		}
		param := method.MethodInfo.Parameters[idx]
		compiled[idx] = compileValue(item, describeJavaType(param.Type, method.ServiceInfo, method.Registry), method.Registry)
	}
	body, err := json.Marshal(compiled)
	if err != nil {
		return nil, fmt.Errorf("encode generic args: %w", err)
	}
	return json.RawMessage(body), nil
}

func compileValue(value any, described javaType, registry facadesemantic.Registry) any {
	switch described.Category {
	case "collection", "array":
		items, ok := value.([]any)
		if !ok {
			return value
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			if described.Element == nil {
				out = append(out, item)
				continue
			}
			out = append(out, compileValue(item, *described.Element, registry))
		}
		return out
	case "map":
		obj, ok := value.(map[string]any)
		if !ok {
			return value
		}
		out := make(map[string]any, len(obj))
		keys := sortedKeys(obj)
		for _, key := range keys {
			if described.Value == nil {
				out[key] = obj[key]
				continue
			}
			out[key] = compileValue(obj[key], *described.Value, registry)
		}
		return out
	case "optional":
		if described.Value == nil {
			return value
		}
		return compileValue(value, *described.Value, registry)
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return value
		}
		out := make(map[string]any, len(obj)+1)
		explicitType, hasExplicitType := obj["@type"].(string)
		if hasExplicitType && strings.TrimSpace(explicitType) != "" {
			out["@type"] = explicitType
		} else if described.FQN != "" {
			out["@type"] = described.FQN
		}
		fields := collectSemanticFields(described.FQN, registry)
		for key, fieldValue := range obj {
			if key == "@type" {
				continue
			}
			fieldType, ok := fields[key]
			if !ok {
				out[key] = fieldValue
				continue
			}
			out[key] = compileValue(fieldValue, fieldType, registry)
		}
		return out
	default:
		return value
	}
}

func collectSemanticFields(fqn string, registry facadesemantic.Registry) map[string]javaType {
	if strings.TrimSpace(fqn) == "" {
		return nil
	}
	classInfo, ok := registry[fqn]
	if !ok {
		return nil
	}
	collected := map[string]javaType{}
	for _, field := range collectInheritedFields(classInfo, registry) {
		collected[field.Name] = describeJavaType(field.JavaType, classInfo, registry)
	}
	return collected
}

func collectInheritedFields(classInfo facadesemantic.SemanticClassInfo, registry facadesemantic.Registry) []facadesemantic.SemanticFieldInfo {
	seen := map[string]struct{}{}
	var chain []facadesemantic.SemanticClassInfo
	current := classInfo
	for i := 0; i < 20; i++ {
		chain = append(chain, current)
		if strings.TrimSpace(current.Superclass) == "" {
			break
		}
		parentFQN := resolveTypeName(current.Superclass, current, registry)
		parent, ok := registry[parentFQN]
		if !ok {
			break
		}
		current = parent
	}
	var out []facadesemantic.SemanticFieldInfo
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

func describeJavaType(typeStr string, owner facadesemantic.SemanticClassInfo, registry facadesemantic.Registry) javaType {
	text := stripWildcard(strings.TrimSpace(typeStr))
	if text == "" {
		return javaType{Category: "unknown", Raw: typeStr}
	}
	if strings.HasSuffix(text, "[]") {
		inner := strings.TrimSpace(strings.TrimSuffix(text, "[]"))
		element := describeJavaType(inner, owner, registry)
		return javaType{Category: "array", Raw: typeStr, Element: &element}
	}
	head := strings.TrimSpace(strings.Split(text, "<")[0])
	args := splitGenericArguments(text)
	resolvedHead := resolveTypeName(head, owner, registry)
	if matchesNamedType(resolvedHead, stringTypes) {
		return javaType{Category: "string", Raw: typeStr, FQN: resolvedHead}
	}
	if matchesNamedType(resolvedHead, booleanTypes) {
		return javaType{Category: "boolean", Raw: typeStr, FQN: resolvedHead}
	}
	if matchesNamedType(resolvedHead, numberTypes) {
		return javaType{Category: "number", Raw: typeStr, FQN: resolvedHead}
	}
	if matchesNamedType(resolvedHead, decimalTypes) {
		return javaType{Category: "decimal", Raw: typeStr, FQN: resolvedHead}
	}
	if matchesNamedType(resolvedHead, dateTypes) {
		return javaType{Category: "date", Raw: typeStr, FQN: resolvedHead}
	}
	if strings.EqualFold(resolvedHead, "java.lang.Object") || resolvedHead == "Object" {
		return javaType{Category: "unknown", Raw: typeStr, FQN: resolvedHead}
	}
	if isOptionalTypeName(resolvedHead) {
		jt := javaType{Category: "optional", Raw: typeStr, FQN: resolvedHead}
		if len(args) > 0 {
			value := describeJavaType(args[0], owner, registry)
			jt.Value = &value
		}
		return jt
	}
	if matchesNamedType(resolvedHead, collectionTypes) {
		jt := javaType{Category: "collection", Raw: typeStr, FQN: resolvedHead}
		if len(args) > 0 {
			element := describeJavaType(args[0], owner, registry)
			jt.Element = &element
		}
		return jt
	}
	if matchesNamedType(resolvedHead, mapTypes) {
		jt := javaType{Category: "map", Raw: typeStr, FQN: resolvedHead}
		if len(args) == 2 {
			key := describeJavaType(args[0], owner, registry)
			value := describeJavaType(args[1], owner, registry)
			jt.Key = &key
			jt.Value = &value
		}
		return jt
	}
	if target, ok := registry[resolvedHead]; ok {
		switch target.Kind {
		case "enum":
			return javaType{Category: "enum", Raw: typeStr, FQN: target.FQN}
		case "class":
			return javaType{Category: "object", Raw: typeStr, FQN: target.FQN}
		}
	}
	return javaType{Category: "unknown", Raw: typeStr, FQN: resolvedHead}
}

func splitGenericArguments(typeStr string) []string {
	start := strings.Index(typeStr, "<")
	end := strings.LastIndex(typeStr, ">")
	if start < 0 || end <= start {
		return nil
	}
	raw := typeStr[start+1 : end]
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
				token := strings.TrimSpace(current.String())
				if token != "" {
					out = append(out, token)
				}
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

func matchesNamedType(typeName string, known map[string]struct{}) bool {
	if _, ok := known[typeName]; ok {
		return true
	}
	if idx := strings.LastIndex(typeName, "."); idx >= 0 {
		_, ok := known[typeName[idx+1:]]
		return ok
	}
	return false
}

func isOptionalTypeName(typeName string) bool {
	return typeName == "java.util.Optional" || typeName == "Optional"
}

func sortedKeys(items map[string]any) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
