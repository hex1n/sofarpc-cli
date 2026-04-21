package sofarpcwire

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

type hessianWriter struct {
	buf        []byte
	objectDefs map[string]int
}

func newHessianWriter() *hessianWriter {
	return &hessianWriter{
		buf:        make([]byte, 0, 512),
		objectDefs: make(map[string]int),
	}
}

func (w *hessianWriter) Bytes() []byte {
	return append([]byte(nil), w.buf...)
}

func (w *hessianWriter) writeSofaRequest(method, targetServiceUniqueName string, requestProps map[string]interface{}, paramTypes []string, targetAppName *string) error {
	values := []any{
		nil,
		method,
		targetServiceUniqueName,
		requestProps,
		append([]string(nil), paramTypes...),
	}
	if targetAppName != nil {
		values[0] = *targetAppName
	}
	return w.writeObject(RequestClass, []string{
		"targetAppName",
		"methodName",
		"targetServiceUniqueName",
		"requestProps",
		"methodArgSigs",
	}, values)
}

func (w *hessianWriter) writeObject(typeName string, fieldNames []string, values []any) error {
	if len(fieldNames) != len(values) {
		return fmt.Errorf("object %s field/value mismatch: %d != %d", typeName, len(fieldNames), len(values))
	}

	key := objectDefKey(typeName, fieldNames)
	ref, ok := w.objectDefs[key]
	if !ok {
		ref = len(w.objectDefs)
		w.objectDefs[key] = ref
		w.buf = append(w.buf, 'O')
		w.writeLenString(typeName)
		w.writeInt(int64(len(fieldNames)))
		for _, name := range fieldNames {
			if err := w.writeString(name); err != nil {
				return err
			}
		}
	}

	w.buf = append(w.buf, 'o')
	w.writeInt(int64(ref))
	for _, value := range values {
		if err := w.writeValue(value); err != nil {
			return err
		}
	}
	return nil
}

func (w *hessianWriter) writeValue(value any) error {
	switch typed := value.(type) {
	case nil:
		w.buf = append(w.buf, 'N')
		return nil
	case bool:
		if typed {
			w.buf = append(w.buf, 'T')
		} else {
			w.buf = append(w.buf, 'F')
		}
		return nil
	case string:
		return w.writeString(typed)
	case int:
		w.writeInt(int64(typed))
		return nil
	case int8:
		w.writeInt(int64(typed))
		return nil
	case int16:
		w.writeInt(int64(typed))
		return nil
	case int32:
		w.writeInt(int64(typed))
		return nil
	case int64:
		w.writeLong(typed)
		return nil
	case uint:
		w.writeLong(int64(typed))
		return nil
	case uint8:
		w.writeLong(int64(typed))
		return nil
	case uint16:
		w.writeLong(int64(typed))
		return nil
	case uint32:
		w.writeLong(int64(typed))
		return nil
	case uint64:
		if typed > math.MaxInt64 {
			return fmt.Errorf("uint64 out of range: %d", typed)
		}
		w.writeLong(int64(typed))
		return nil
	case float32:
		w.writeDouble(float64(typed))
		return nil
	case float64:
		w.writeDouble(typed)
		return nil
	case []string:
		return w.writeTypedList("[string]", stringsToAny(typed))
	case javaTypedObject:
		return w.writeTypedObject(typed)
	case javaLinkedHashMap:
		return w.writeMap("java.util.LinkedHashMap", map[string]interface{}(typed))
	case map[string]interface{}:
		return w.writeMap("", typed)
	case *javaArraysArrayList:
		return w.writeTypedList(typed.JavaClassName(), typed.Get())
	case []any:
		return w.writeTypedList("java.util.Arrays$ArrayList", typed)
	default:
		return fmt.Errorf("unsupported hessian value type %T", value)
	}
}

func (w *hessianWriter) writeTypedObject(obj javaTypedObject) error {
	fieldNames := orderedObjectFieldKeys(obj.fields)
	values := make([]any, 0, len(fieldNames))
	for _, name := range fieldNames {
		values = append(values, obj.fields[name])
	}
	return w.writeObject(obj.typeName, fieldNames, values)
}

func (w *hessianWriter) writeMap(typeName string, values map[string]interface{}) error {
	w.buf = append(w.buf, 'M')
	if typeName != "" {
		w.writeType(typeName)
	}
	for _, key := range orderedMapKeys(values) {
		if err := w.writeString(key); err != nil {
			return err
		}
		if err := w.writeValue(values[key]); err != nil {
			return err
		}
	}
	w.buf = append(w.buf, 'z')
	return nil
}

func (w *hessianWriter) writeTypedList(typeName string, values []interface{}) error {
	w.buf = append(w.buf, 'V')
	if typeName != "" {
		w.writeType(typeName)
	}
	w.writeLength(len(values))
	for _, value := range values {
		if err := w.writeValue(value); err != nil {
			return err
		}
	}
	w.buf = append(w.buf, 'z')
	return nil
}

func (w *hessianWriter) writeType(typeName string) {
	w.buf = append(w.buf, 't')
	w.writeUint16(uint16(utf8.RuneCountInString(typeName)))
	w.buf = append(w.buf, []byte(typeName)...)
}

func (w *hessianWriter) writeLenString(value string) {
	w.writeInt(int64(utf8.RuneCountInString(value)))
	w.buf = append(w.buf, []byte(value)...)
}

func (w *hessianWriter) writeString(value string) error {
	if value == "" {
		w.buf = append(w.buf, byte(0))
		return nil
	}

	length := utf8.RuneCountInString(value)
	if length <= 0x1f {
		w.buf = append(w.buf, byte(length))
		w.buf = append(w.buf, []byte(value)...)
		return nil
	}
	if length > math.MaxUint16 {
		return fmt.Errorf("string too long: %d chars", length)
	}
	w.buf = append(w.buf, 'S')
	w.writeUint16(uint16(length))
	w.buf = append(w.buf, []byte(value)...)
	return nil
}

func (w *hessianWriter) writeLength(length int) {
	if length >= 0 && length <= 0xff {
		w.buf = append(w.buf, hessianLengthByte, byte(length))
		return
	}
	w.buf = append(w.buf, 'l')
	w.writeUint32(uint32(length))
}

func (w *hessianWriter) writeInt(value int64) {
	w.buf = append(w.buf, 'I')
	w.writeUint32(uint32(int32(value)))
}

func (w *hessianWriter) writeLong(value int64) {
	w.buf = append(w.buf, 'L')
	w.writeUint64(uint64(value))
}

func (w *hessianWriter) writeDouble(value float64) {
	w.buf = append(w.buf, 'D')
	w.writeUint64(math.Float64bits(value))
}

func (w *hessianWriter) writeUint16(value uint16) {
	w.buf = append(w.buf, byte(value>>8), byte(value))
}

func (w *hessianWriter) writeUint32(value uint32) {
	w.buf = append(w.buf,
		byte(value>>24),
		byte(value>>16),
		byte(value>>8),
		byte(value),
	)
}

func (w *hessianWriter) writeUint64(value uint64) {
	w.buf = append(w.buf,
		byte(value>>56),
		byte(value>>48),
		byte(value>>40),
		byte(value>>32),
		byte(value>>24),
		byte(value>>16),
		byte(value>>8),
		byte(value),
	)
}

func objectDefKey(typeName string, fieldNames []string) string {
	return typeName + "\x00" + strings.Join(fieldNames, "\x00")
}

func orderedMapKeys(values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}

	preferred := []string{"@type", "tradeDate", "mpCode", "mpCodeList"}
	keys := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, key := range preferred {
		if _, ok := values[key]; ok {
			keys = append(keys, key)
			seen[key] = struct{}{}
		}
	}
	extra := make([]string, 0, len(values)-len(keys))
	for key := range values {
		if _, ok := seen[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

func orderedObjectFieldKeys(values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	preferred := []string{
		"tradeDate",
		"mpCode",
		"mpCodeList",
		"fundCode",
		"tradeType",
		"tradeValue",
		"shareInTransit",
		"tradeFeeRules",
		"conditionExpression",
		"feeType",
		"feeValue",
		"discountFeeValue",
		"discount",
		"value",
	}
	keys := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, key := range preferred {
		if _, ok := values[key]; ok {
			keys = append(keys, key)
			seen[key] = struct{}{}
		}
	}
	extra := make([]string, 0, len(values)-len(keys))
	for key := range values {
		if _, ok := seen[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

func stringsToAny(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
