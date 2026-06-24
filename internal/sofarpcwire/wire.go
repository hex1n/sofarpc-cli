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

	DefaultVersion    = "1.0"
	GenericType       = "2"
	InvokeTypeSync    = "sync"
	RequestBaggageKey = "rpc_req_baggage"
)

type RequestSpec struct {
	Service    string
	Method     string
	ParamTypes []string
	// Args are contract-normalized JSON-like values. BuildGenericRequest calls
	// PrepareArgs to adapt typed maps and collections for Hessian encoding.
	Args           []any
	Version        string
	UniqueID       string
	TargetAppName  string
	RequestBaggage map[string]string
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

	preparedArgs := PrepareArgs(spec.Args)
	requestProps := fixedGenericRequestProps()
	if err := addRequestBaggage(requestProps, spec.RequestBaggage); err != nil {
		return EncodedRequest{}, err
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
	for i, arg := range preparedArgs {
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

func fixedGenericRequestProps() map[string]interface{} {
	return map[string]interface{}{
		"sofa_head_generic_type": GenericType,
		"type":                   InvokeTypeSync,
		"generic.revise":         "true",
	}
}

func addRequestBaggage(requestProps map[string]interface{}, baggage map[string]string) error {
	if len(baggage) == 0 {
		return nil
	}
	keys := make([]string, 0, len(baggage))
	for key := range baggage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seen := map[string]struct{}{}
	requestBaggage := make(map[string]interface{}, len(baggage))
	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		if err := validateRequestBaggageKey(key); err != nil {
			return err
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("request baggage key %q is duplicated after trimming", key)
		}
		seen[key] = struct{}{}
		requestBaggage[key] = baggage[rawKey]
	}
	requestProps[RequestBaggageKey] = requestBaggage
	return nil
}

func validateRequestBaggageKey(key string) error {
	if key == "" {
		return fmt.Errorf("request baggage key must not be empty")
	}
	return nil
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

// PrepareArgs adapts contract-normalized JSON-like args for the Hessian writer
// by replacing typed maps and collections with Java class adapters.
func PrepareArgs(args []any) []any {
	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = PrepareValue(arg)
	}
	return out
}

// PrepareValue adapts one contract-normalized value for Hessian encoding.
func PrepareValue(v any) any {
	return normalizeValue(v)
}

// NormalizeArgs is kept for compatibility with existing tests and callers.
// New code should use PrepareArgs to avoid confusion with contract.NormalizeArgs.
func NormalizeArgs(args []any) []any {
	return PrepareArgs(args)
}

func FormatValue(v any) any {
	out, err := FormatValueSafe(v)
	if err != nil {
		return map[string]any{
			"error": err.Error(),
		}
	}
	return out
}

func FormatValueSafe(v any) (out any, err error) {
	state := formatState{stack: map[formatVisit]string{}}
	defer func() {
		if recovered := recover(); recovered != nil {
			out = nil
			err = fmt.Errorf("format value panic: %v", recovered)
		}
	}()
	return state.formatValue(v, "result", 0)
}

type formatVisit struct {
	typ  reflect.Type
	kind reflect.Kind
	ptr  uintptr
}

type formatState struct {
	stack map[formatVisit]string
}

func (s *formatState) formatValue(v any, path string, depth int) (any, error) {
	if depth > defaultHessianMaxDepth {
		return nil, fmt.Errorf("format value depth at %s exceeds limit %d", path, defaultHessianMaxDepth)
	}
	switch value := v.(type) {
	case nil, bool, string, int64, float64, int32, int16, int8, int, uint64, uint32, uint16, uint8, uint:
		return value, nil
	case map[string]interface{}:
		return s.formatStringMap(value, path, depth)
	case map[interface{}]interface{}:
		leave, err := s.enter(value, path)
		if err != nil {
			return nil, err
		}
		defer leave()
		converted := make(map[string]interface{}, len(value))
		for key, item := range value {
			converted[fmt.Sprint(key)] = item
		}
		return s.formatStringMap(converted, path, depth)
	case []any:
		leave, err := s.enter(value, path)
		if err != nil {
			return nil, err
		}
		defer leave()
		out := make([]any, len(value))
		for i, item := range value {
			formatted, err := s.formatValue(item, fmt.Sprintf("%s[%d]", path, i), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = formatted
		}
		return out, nil
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	case []int64:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	case []float64:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	case []bool:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		leave, err := s.enterReflect(rv, path)
		if err != nil {
			return nil, err
		}
		defer leave()
		out := make([]any, rv.Len())
		for i := range out {
			item := rv.Index(i)
			if !item.CanInterface() {
				return nil, fmt.Errorf("format value at %s[%d]: value cannot be represented as interface", path, i)
			}
			formatted, err := s.formatValue(item.Interface(), fmt.Sprintf("%s[%d]", path, i), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = formatted
		}
		return out, nil
	case reflect.Map:
		leave, err := s.enterReflect(rv, path)
		if err != nil {
			return nil, err
		}
		defer leave()
		converted := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			value := iter.Value()
			if !key.CanInterface() || !value.CanInterface() {
				return nil, fmt.Errorf("format value at %s: map key/value cannot be represented as interface", path)
			}
			converted[fmt.Sprint(key.Interface())] = value.Interface()
		}
		return s.formatStringMap(converted, path, depth)
	default:
		return v, nil
	}
}

func (s *formatState) enter(v any, path string) (func(), error) {
	return s.enterReflect(reflect.ValueOf(v), path)
}

func (s *formatState) enterReflect(rv reflect.Value, path string) (func(), error) {
	key, ok := formatVisitKey(rv)
	if !ok {
		return func() {}, nil
	}
	if existing, seen := s.stack[key]; seen {
		return nil, fmt.Errorf("format value cycle detected at %s; already visited at %s", path, existing)
	}
	s.stack[key] = path
	return func() {
		delete(s.stack, key)
	}, nil
}

func formatVisitKey(rv reflect.Value) (formatVisit, bool) {
	if !rv.IsValid() {
		return formatVisit{}, false
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Slice:
		if rv.IsNil() {
			return formatVisit{}, false
		}
		return formatVisit{typ: rv.Type(), kind: rv.Kind(), ptr: rv.Pointer()}, true
	default:
		return formatVisit{}, false
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
	out, err := (&formatState{stack: map[formatVisit]string{}}).formatStringMap(input, "result", 0)
	if err != nil {
		return map[string]any{
			"error": err.Error(),
		}
	}
	return out
}

func (s *formatState) formatStringMap(input map[string]interface{}, path string, depth int) (any, error) {
	leave, err := s.enter(input, path)
	if err != nil {
		return nil, err
	}
	defer leave()

	if className, ok := input[javaTypeKey].(string); ok && className != "" {
		fields := make(map[string]any, len(input)-1)
		fieldNames := make([]string, 0, len(input)-1)
		for key, value := range input {
			if key == javaTypeKey {
				continue
			}
			formatted, err := s.formatValue(value, path+"."+key, depth+1)
			if err != nil {
				return nil, err
			}
			fields[key] = formatted
			fieldNames = append(fieldNames, key)
		}
		sort.Strings(fieldNames)
		return map[string]any{
			"type":       className,
			"fields":     fields,
			"fieldNames": fieldNames,
		}, nil
	}

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(input))
	for _, key := range keys {
		formatted, err := s.formatValue(input[key], path+"."+key, depth+1)
		if err != nil {
			return nil, err
		}
		out[key] = formatted
	}
	return out, nil
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
