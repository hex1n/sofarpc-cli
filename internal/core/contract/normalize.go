package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	"github.com/hex1n/sofarpc-cli/internal/javatype"
)

func NormalizeArgs(paramTypes []string, args []any, store Store) ([]any, error) {
	if len(paramTypes) != len(args) {
		return nil, fmt.Errorf("arity mismatch: got %d args for %d paramTypes", len(args), len(paramTypes))
	}
	lookup := ClassLookup(store)
	out := make([]any, len(args))
	for i, arg := range args {
		normalized, err := normalizeForType(ParseTypeSpec(paramTypes[i]), arg, store, lookup)
		if err != nil {
			return nil, fmt.Errorf("arg %d (%s): %w", i, paramTypes[i], err)
		}
		out[i] = normalized
	}
	return out, nil
}

func normalizeForType(spec TypeSpec, raw any, store Store, lookup javatype.ClassLookup) (any, error) {
	if raw == nil {
		return nil, nil
	}
	if spec.IsZero() {
		return normalizeLoose(raw, store, lookup)
	}
	if spec.Wildcard != WildcardNone {
		spec = spec.Effective()
	}
	if spec.Base == "" || spec.Base == "java.lang.Object" {
		return normalizeLoose(raw, store, lookup)
	}
	if spec.ArrayDepth > 0 {
		items, ok := sliceValues(raw)
		if !ok {
			return nil, fmt.Errorf("expected array/slice for %s, got %T", spec.Base, raw)
		}
		child := spec.Element()
		out := make([]any, len(items))
		for i, item := range items {
			normalized, err := normalizeForType(child, item, store, lookup)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			out[i] = normalized
		}
		return out, nil
	}

	if cls, ok := store.Class(spec.Base); ok && cls.Kind == javamodel.KindEnum {
		return normalizeEnum(raw), nil
	}

	role := javatype.Classify(spec.Base, lookup)
	switch role {
	case javatype.RoleContainer:
		if javatype.Placeholder(spec.Base) == javatype.PlaceholderMap {
			return normalizeMap(spec, raw, store, lookup)
		}
		return normalizeCollection(spec, raw, store, lookup)
	case javatype.RolePassthrough:
		return normalizeScalar(spec.Base, raw)
	case javatype.RoleUserType:
		return normalizeObject(spec, raw, store, lookup)
	default:
		return normalizeLoose(raw, store, lookup)
	}
}

func normalizeCollection(spec TypeSpec, raw any, store Store, lookup javatype.ClassLookup) (any, error) {
	items, ok := sliceValues(raw)
	if !ok {
		return nil, fmt.Errorf("expected collection for %s, got %T", spec.Base, raw)
	}
	if len(spec.Args) == 0 {
		return normalizeLoose(raw, store, lookup)
	}
	out := make([]any, len(items))
	for i, item := range items {
		normalized, err := normalizeForType(spec.Args[0], item, store, lookup)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out[i] = normalized
	}
	return out, nil
}

func normalizeMap(spec TypeSpec, raw any, store Store, lookup javatype.ClassLookup) (any, error) {
	values, ok := stringMap(raw)
	if !ok {
		return nil, fmt.Errorf("expected object for %s, got %T", spec.Base, raw)
	}
	if len(spec.Args) < 2 {
		return normalizeLoose(values, store, lookup)
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		normalized, err := normalizeForType(spec.Args[1], value, store, lookup)
		if err != nil {
			return nil, fmt.Errorf("map value %q: %w", key, err)
		}
		out[key] = normalized
	}
	return out, nil
}

func normalizeObject(spec TypeSpec, raw any, store Store, lookup javatype.ClassLookup) (any, error) {
	values, ok := stringMap(raw)
	if !ok {
		return nil, fmt.Errorf("expected object for %s, got %T", spec.Base, raw)
	}

	typeName := spec.Base
	if explicit, ok := values["@type"].(string); ok && strings.TrimSpace(explicit) != "" {
		typeName = strings.TrimSpace(explicit)
		if err := validateExplicitType(spec.Base, typeName, store); err != nil {
			return nil, err
		}
	}
	fieldTypes := resolvedFieldMap(store, typeName)
	out := make(map[string]any, len(values)+1)
	out["@type"] = typeName

	for key, value := range values {
		if key == "@type" {
			continue
		}
		field, ok := fieldTypes[key]
		if !ok {
			normalized, err := normalizeLoose(value, store, lookup)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", key, err)
			}
			out[key] = normalized
			continue
		}
		normalized, err := normalizeForType(ParseTypeSpec(field.JavaType), value, store, lookup)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", key, err)
		}
		out[key] = normalized
	}
	return out, nil
}

func normalizeEnum(raw any) any {
	if value, ok := raw.(string); ok {
		return value
	}
	return raw
}

func normalizeScalar(base string, raw any) (any, error) {
	switch strings.TrimSpace(base) {
	case "boolean", "java.lang.Boolean":
		return normalizeBool(raw), nil
	case "byte", "java.lang.Byte":
		if value, ok := toInt64(raw); ok && value >= math.MinInt8 && value <= math.MaxInt8 {
			return int8(value), nil
		}
	case "short", "java.lang.Short":
		if value, ok := toInt64(raw); ok && value >= math.MinInt16 && value <= math.MaxInt16 {
			return int16(value), nil
		}
	case "int", "java.lang.Integer":
		if value, ok := toInt64(raw); ok && value >= math.MinInt32 && value <= math.MaxInt32 {
			return int32(value), nil
		}
	case "long", "java.lang.Long":
		if value, ok := toInt64(raw); ok {
			return value, nil
		}
	case "float", "java.lang.Float":
		if value, ok := toFloat64(raw); ok {
			return float32(value), nil
		}
	case "double", "java.lang.Double":
		if value, ok := toFloat64(raw); ok {
			return value, nil
		}
	case "java.math.BigDecimal", "java.math.BigInteger":
		if values, ok := stringMap(raw); ok {
			if explicit, ok := values["@type"].(string); ok && strings.TrimSpace(explicit) != "" && strings.TrimSpace(explicit) != base {
				return nil, fmt.Errorf("explicit @type %q is not assignable to declared type %s", strings.TrimSpace(explicit), base)
			}
		}
		if value, ok := decimalObjectFor(base, raw); ok {
			return value, nil
		}
	}
	return raw, nil
}

func validateExplicitType(declared, actual string, store Store) error {
	declared = rawJavaTypeName(declared)
	actual = rawJavaTypeName(actual)
	if declared == "" || actual == "" || declared == "java.lang.Object" || declared == actual {
		return nil
	}
	if typeAssignable(store, actual, declared) {
		return nil
	}
	return fmt.Errorf("explicit @type %q is not assignable to declared type %s", actual, declared)
}

func typeAssignable(store Store, actual, declared string) bool {
	if store == nil {
		return false
	}
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(name string) bool {
		name = rawJavaTypeName(name)
		if name == "" || seen[name] {
			return false
		}
		if name == declared {
			return true
		}
		seen[name] = true
		cls, ok := store.Class(name)
		if !ok {
			return false
		}
		if walk(cls.Superclass) {
			return true
		}
		for _, iface := range cls.Interfaces {
			if walk(iface) {
				return true
			}
		}
		return false
	}
	return walk(actual)
}

func rawJavaTypeName(name string) string {
	spec := ParseTypeSpec(strings.TrimSpace(name))
	if spec.Base == "" {
		return strings.TrimSpace(name)
	}
	return spec.Base
}

func normalizeBool(raw any) any {
	if value, ok := raw.(bool); ok {
		return value
	}
	if value, ok := raw.(string); ok {
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "true":
			return true
		case "false":
			return false
		}
	}
	return raw
}

func normalizeLoose(raw any, store Store, lookup javatype.ClassLookup) (any, error) {
	if raw == nil {
		return nil, nil
	}
	if values, ok := stringMap(raw); ok {
		if explicit, ok := values["@type"].(string); ok && strings.TrimSpace(explicit) != "" {
			return normalizeForType(ParseTypeSpec(explicit), values, store, lookup)
		}
		out := make(map[string]any, len(values))
		for key, value := range values {
			normalized, err := normalizeLoose(value, store, lookup)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	}
	if items, ok := sliceValues(raw); ok {
		out := make([]any, len(items))
		for i, item := range items {
			normalized, err := normalizeLoose(item, store, lookup)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	}
	return raw, nil
}

func decimalObjectFor(base string, raw any) (map[string]any, bool) {
	if values, ok := stringMap(raw); ok {
		typeName := base
		if explicit, ok := values["@type"].(string); ok && strings.TrimSpace(explicit) != "" {
			typeName = strings.TrimSpace(explicit)
		}
		if value, ok := values["value"]; ok {
			if asString, ok := decimalString(value); ok {
				return map[string]any{"@type": typeName, "value": asString}, true
			}
		}
		return nil, false
	}
	if asString, ok := decimalString(raw); ok {
		return map[string]any{"@type": base, "value": asString}, true
	}
	return nil, false
}

func decimalString(raw any) (string, bool) {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value), true
	case json.Number:
		return value.String(), true
	case int:
		return strconv.FormatInt(int64(value), 10), true
	case int8:
		return strconv.FormatInt(int64(value), 10), true
	case int16:
		return strconv.FormatInt(int64(value), 10), true
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint:
		return strconv.FormatUint(uint64(value), 10), true
	case uint8:
		return strconv.FormatUint(uint64(value), 10), true
	case uint16:
		return strconv.FormatUint(uint64(value), 10), true
	case uint32:
		return strconv.FormatUint(uint64(value), 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	default:
		return "", false
	}
}

func toInt64(raw any) (int64, bool) {
	switch value := raw.(type) {
	case json.Number:
		v, err := value.Int64()
		return v, err == nil
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		if uint64(value) > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		if value > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case float32:
		if math.Trunc(float64(value)) != float64(value) {
			return 0, false
		}
		return int64(value), true
	case float64:
		if math.Trunc(value) != value || value < math.MinInt64 || value > math.MaxInt64 {
			return 0, false
		}
		return int64(value), true
	case string:
		v, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return v, err == nil
	default:
		return 0, false
	}
}

func toFloat64(raw any) (float64, bool) {
	switch value := raw.(type) {
	case json.Number:
		v, err := value.Float64()
		return v, err == nil
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case string:
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return v, err == nil
	default:
		return 0, false
	}
}

func sliceValues(raw any) ([]any, bool) {
	switch values := raw.(type) {
	case []any:
		return values, true
	case []string:
		out := make([]any, len(values))
		for i, value := range values {
			out[i] = value
		}
		return out, true
	}
	rv := reflect.ValueOf(raw)
	if !rv.IsValid() {
		return nil, false
	}
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

func stringMap(raw any) (map[string]any, bool) {
	switch values := raw.(type) {
	case map[string]any:
		return values, true
	}

	rv := reflect.ValueOf(raw)
	if !rv.IsValid() || rv.Kind() != reflect.Map {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[fmt.Sprint(iter.Key().Interface())] = iter.Value().Interface()
	}
	return out, true
}
