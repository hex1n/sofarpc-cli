package sofarpcwire

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
)

const (
	RequestClass  = "com.alipay.sofa.rpc.core.request.SofaRequest"
	ResponseClass = "com.alipay.sofa.rpc.core.response.SofaResponse"
	javaTypeKey   = "@type"

	DefaultVersion = "1.0"
	GenericType    = "2"
	InvokeTypeSync = "sync"
)

type RequestSpec struct {
	Service       string
	Method        string
	ParamTypes    []string
	Args          []any
	Version       string
	UniqueID      string
	TargetAppName string
}

type EncodedRequest struct {
	Class                   string
	Header                  map[string]string
	Content                 []byte
	TargetServiceUniqueName string
}

type DecodedResponse struct {
	IsError       bool
	ErrorMsg      string
	AppResponse   any
	ResponseProps map[string]string
}

type javaLinkedHashMap map[string]interface{}

func (javaLinkedHashMap) JavaClassName() string { return "java.util.LinkedHashMap" }

type javaTypedObject struct {
	typeName string
	fields   map[string]interface{}
}

// javaArrayList carries a Go slice that should land on the wire as
// java.util.ArrayList. We use this instead of the Arrays$ArrayList
// JDK-internal class because some SOFARPC servers' hessian2 factories
// don't register the internal type and fall back to Map-of-index or fail
// to decode; java.util.ArrayList is the lowest-common-denominator shape.
type javaArrayList struct {
	values []interface{}
}

func (*javaArrayList) JavaClassName() string { return "java.util.ArrayList" }

func (j *javaArrayList) Get() []interface{} { return append([]interface{}(nil), j.values...) }

func (j *javaArrayList) Set(values []interface{}) {
	j.values = append([]interface{}(nil), values...)
}

func BuildGenericRequest(spec RequestSpec) (EncodedRequest, error) {
	if strings.TrimSpace(spec.Service) == "" {
		return EncodedRequest{}, fmt.Errorf("service is required")
	}
	if strings.TrimSpace(spec.Method) == "" {
		return EncodedRequest{}, fmt.Errorf("method is required")
	}
	if len(spec.ParamTypes) != len(spec.Args) {
		return EncodedRequest{}, fmt.Errorf("paramTypes/args length mismatch: %d != %d", len(spec.ParamTypes), len(spec.Args))
	}

	targetServiceUniqueName := TargetServiceUniqueName(spec.Service, spec.Version, spec.UniqueID)
	header := requestHeader(spec.Method, targetServiceUniqueName, spec.TargetAppName)

	normalizedArgs := NormalizeArgs(spec.Args)
	requestProps := map[string]interface{}{
		"sofa_head_generic_type": GenericType,
		"type":                   InvokeTypeSync,
		"generic.revise":         "true",
	}
	var targetAppName *string
	if strings.TrimSpace(spec.TargetAppName) != "" {
		value := strings.TrimSpace(spec.TargetAppName)
		targetAppName = &value
	}

	enc := newHessianWriter()
	if err := enc.writeSofaRequest(spec.Method, targetServiceUniqueName, requestProps, spec.ParamTypes, targetAppName); err != nil {
		return EncodedRequest{}, fmt.Errorf("encode SofaRequest: %w", err)
	}
	for i, arg := range normalizedArgs {
		if err := enc.writeValue(arg); err != nil {
			return EncodedRequest{}, fmt.Errorf("encode arg %d: %w", i, err)
		}
	}

	return EncodedRequest{
		Class:                   RequestClass,
		Header:                  header,
		Content:                 enc.Bytes(),
		TargetServiceUniqueName: targetServiceUniqueName,
	}, nil
}

func DecodeResponse(content []byte) (DecodedResponse, error) {
	return decodeSofaResponse(content)
}

func TargetServiceUniqueName(service, version, uniqueID string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = DefaultVersion
	}
	uniqueName := strings.TrimSpace(service) + ":" + version
	if strings.TrimSpace(uniqueID) != "" {
		uniqueName += ":" + strings.TrimSpace(uniqueID)
	}
	return uniqueName
}

func LoadArgsFile(path string) ([]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var raw []any
	dec := json.NewDecoder(f)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// NormalizeArgs prepares canonical JSON-like args for the Hessian writer by
// replacing maps/slices with Java class adapters. Contract-aware semantic
// normalization happens earlier in internal/core/contract.
func NormalizeArgs(args []any) []any {
	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = normalizeValue(arg)
	}
	return out
}

func FormatValue(v any) any {
	switch value := v.(type) {
	case nil, bool, string, int64, float64, int32, int16, int8, int, uint64, uint32, uint16, uint8, uint:
		return value
	case map[string]interface{}:
		return formatStringMap(value)
	case map[interface{}]interface{}:
		converted := make(map[string]interface{}, len(value))
		for key, item := range value {
			converted[fmt.Sprint(key)] = item
		}
		return formatStringMap(converted)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = FormatValue(item)
		}
		return out
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []int64:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []float64:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []bool:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = FormatValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		converted := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			converted[fmt.Sprint(iter.Key().Interface())] = iter.Value().Interface()
		}
		return formatStringMap(converted)
	default:
		return v
	}
}

func normalizeValue(v any) any {
	switch value := v.(type) {
	case json.Number:
		return normalizeNumber(value)
	case []any:
		return normalizeSlice(value)
	case map[string]interface{}:
		return normalizeStringMap(value)
	default:
		return value
	}
}

func normalizeNumber(n json.Number) any {
	raw := n.String()
	if strings.ContainsAny(raw, ".eE") {
		if value, err := n.Float64(); err == nil {
			return value
		}
		return raw
	}
	if value, err := n.Int64(); err == nil {
		return value
	}
	if value, err := n.Float64(); err == nil {
		return value
	}
	return raw
}

func normalizeSlice(items []any) any {
	out := make([]interface{}, len(items))
	for i, item := range items {
		out[i] = normalizeValue(item)
	}
	return &javaArrayList{values: out}
}

func normalizeStringMap(input map[string]interface{}) any {
	if className, ok := input[javaTypeKey].(string); ok && strings.TrimSpace(className) != "" {
		fields := make(map[string]interface{}, len(input)-1)
		for key, value := range input {
			if key == javaTypeKey {
				continue
			}
			fields[key] = normalizeValue(value)
		}
		return javaTypedObject{
			typeName: strings.TrimSpace(className),
			fields:   fields,
		}
	}
	out := make(javaLinkedHashMap, len(input))
	for key, value := range input {
		out[key] = normalizeValue(value)
	}
	return out
}

func formatStringMap(input map[string]interface{}) any {
	if className, ok := input[javaTypeKey].(string); ok && className != "" {
		fields := make(map[string]any, len(input)-1)
		fieldNames := make([]string, 0, len(input)-1)
		for key, value := range input {
			if key == javaTypeKey {
				continue
			}
			fields[key] = FormatValue(value)
			fieldNames = append(fieldNames, key)
		}
		sort.Strings(fieldNames)
		return map[string]any{
			"type":       className,
			"fields":     fields,
			"fieldNames": fieldNames,
		}
	}

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(input))
	for _, key := range keys {
		out[key] = FormatValue(input[key])
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

func requestHeader(method, targetServiceUniqueName, targetAppName string) map[string]string {
	header := map[string]string{
		"service":                  targetServiceUniqueName,
		"sofa_head_method_name":    method,
		"sofa_head_target_service": targetServiceUniqueName,
		"sofa_head_generic_type":   GenericType,
		"type":                     InvokeTypeSync,
		"generic.revise":           "true",
	}
	if strings.TrimSpace(targetAppName) != "" {
		header["sofa_head_target_app"] = targetAppName
	}
	return header
}
