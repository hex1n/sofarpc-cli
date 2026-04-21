package sofarpcwire

import (
	"fmt"
	"math"
	"unicode/utf8"
)

const (
	hessianIntZero       = 0x90
	hessianIntByteZero   = 0xc8
	hessianIntShortZero  = 0xd4
	hessianLongZero      = 0xe0
	hessianLongByteZero  = 0xf8
	hessianLongShortZero = 0x3c

	hessianLengthByte  = 0x6e
	hessianLongInt     = 0x77
	hessianTypeRef     = 0x75
	hessianRefByte     = 0x4a
	hessianRefShort    = 0x4b
	hessianDoubleZero  = 0x67
	hessianDoubleOne   = 0x68
	hessianDoubleByte  = 0x69
	hessianDoubleShort = 0x6a
	hessianDoubleFloat = 0x6b
)

type hessianClassDef struct {
	Type       string
	FieldNames []string
}

type hessianDecoder struct {
	data      []byte
	offset    int
	refs      []any
	classDefs []hessianClassDef
	types     []string
}

func decodeSofaResponse(content []byte) (DecodedResponse, error) {
	decoder := hessianDecoder{data: content}
	value, err := decoder.readValue()
	if err != nil {
		return DecodedResponse{}, fmt.Errorf("decode SofaResponse: %w", err)
	}

	fields, typeName, ok := typedObject(value)
	if !ok {
		return DecodedResponse{}, fmt.Errorf("unexpected response type %T", value)
	}
	if typeName != ResponseClass {
		return decodeRemoteException(typeName, fields), nil
	}

	resp := DecodedResponse{
		AppResponse:   fields["appResponse"],
		ResponseProps: toStringMap(fields["responseProps"]),
	}
	if isError, ok := fields["isError"].(bool); ok {
		resp.IsError = isError
	}
	if errorMsg, ok := fields["errorMsg"].(string); ok {
		resp.ErrorMsg = errorMsg
	}
	return resp, nil
}

func typedObject(value any) (map[string]any, string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, "", false
	}
	gotType, _ := object["type"].(string)
	fields, ok := object["fields"].(map[string]any)
	if !ok {
		return nil, gotType, false
	}
	return fields, gotType, true
}

func decodeRemoteException(typeName string, fields map[string]any) DecodedResponse {
	resp := DecodedResponse{
		IsError:     true,
		AppResponse: map[string]any{"type": typeName, "fields": fields},
	}
	resp.ErrorMsg = firstNonEmptyString(
		stringField(fields, "message"),
		stringField(fields, "errorMsg"),
		stringField(fields, "msg"),
		stringField(fields, "detailMessage"),
		stringField(fields, "localizedMessage"),
	)
	if resp.ErrorMsg == "" {
		if causeType := nestedTypeField(fields, "cause"); causeType != "" {
			resp.ErrorMsg = typeName + ": cause=" + causeType
		} else {
			resp.ErrorMsg = typeName
		}
	}
	if stack, ok := fields["stackTrace"]; ok {
		resp.ResponseProps = map[string]string{
			"remoteExceptionType": typeName,
			"stackTrace":          summarizeValue(stack),
		}
		return resp
	}
	resp.ResponseProps = map[string]string{
		"remoteExceptionType": typeName,
	}
	return resp
}

func stringField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int64:
		return fmt.Sprintf("%d", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nestedTypeField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	typeName, _ := obj["type"].(string)
	return typeName
}

func summarizeValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		return fmt.Sprintf("list(len=%d)", len(typed))
	case map[string]any:
		if typeName, _ := typed["type"].(string); typeName != "" {
			return "object(" + typeName + ")"
		}
		if entries, ok := typed["entries"].(map[string]any); ok {
			return fmt.Sprintf("map(len=%d)", len(entries))
		}
		return fmt.Sprintf("map(len=%d)", len(typed))
	default:
		return fmt.Sprintf("%T", value)
	}
}

func toStringMap(value any) map[string]string {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	case map[string]any:
		if entries, ok := typed["entries"].(map[string]any); ok {
			return toStringMap(entries)
		}
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = fmt.Sprint(item)
		}
		return out
	default:
		return map[string]string{
			"value": fmt.Sprint(value),
		}
	}
}

func (d *hessianDecoder) readValue() (any, error) {
	tag, err := d.readByte()
	if err != nil {
		return nil, err
	}

	switch {
	case tag == 'N':
		return nil, nil
	case tag == 'T':
		return true, nil
	case tag == 'F':
		return false, nil
	case tag >= 0x80 && tag <= 0xbf:
		return int64(int(tag) - hessianIntZero), nil
	case tag >= 0xc0 && tag <= 0xcf:
		next, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-hessianIntByteZero)<<8 | int(next)), nil
	case tag >= 0xd0 && tag <= 0xd7:
		b1, err := d.readByte()
		if err != nil {
			return nil, err
		}
		b2, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-hessianIntShortZero)<<16 | int(b1)<<8 | int(b2)), nil
	case tag == 'I':
		value, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return int64(value), nil
	case tag >= 0xd8 && tag <= 0xef:
		return int64(int(tag) - hessianLongZero), nil
	case tag >= 0xf0 && tag <= 0xff:
		next, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-hessianLongByteZero)<<8 | int(next)), nil
	case tag >= 0x38 && tag <= 0x3f:
		b1, err := d.readByte()
		if err != nil {
			return nil, err
		}
		b2, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return int64((int(tag)-hessianLongShortZero)<<16 | int(b1)<<8 | int(b2)), nil
	case tag == hessianLongInt:
		value, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return int64(value), nil
	case tag == 'L':
		return d.readInt64()
	case tag == hessianDoubleZero:
		return float64(0), nil
	case tag == hessianDoubleOne:
		return float64(1), nil
	case tag == hessianDoubleByte:
		b, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return float64(int8(b)), nil
	case tag == hessianDoubleShort:
		b1, err := d.readByte()
		if err != nil {
			return nil, err
		}
		b2, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return float64(int16(uint16(b1)<<8 | uint16(b2))), nil
	case tag == hessianDoubleFloat:
		value, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(uint32(value))), nil
	case tag == 'D':
		value, err := d.readInt64()
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(uint64(value)), nil
	case tag == 'd':
		return d.readInt64()
	case tag == 'S' || tag == 's' || tag <= 0x1f:
		return d.readStringWithTag(tag)
	case tag == 'B' || tag == 'b' || (tag >= 0x20 && tag <= 0x2f):
		return d.readBytesWithTag(tag)
	case tag == 'V':
		return d.readList()
	case tag == 'v':
		return d.readFixedTypedList()
	case tag == 'M':
		return d.readMap()
	case tag == 'O':
		if err := d.readObjectDefinition(); err != nil {
			return nil, err
		}
		return d.readValue()
	case tag == 'o':
		return d.readObjectInstance()
	case tag == 'R':
		ref, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return d.readRef(ref)
	case tag == hessianRefByte:
		ref, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readRef(int(ref))
	case tag == hessianRefShort:
		b1, err := d.readByte()
		if err != nil {
			return nil, err
		}
		b2, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readRef(int(uint16(b1)<<8 | uint16(b2)))
	default:
		return nil, d.errorf("unsupported tag 0x%02x (%q)", tag, rune(tag))
	}
}

func (d *hessianDecoder) readList() (any, error) {
	typeName, err := d.readType()
	if err != nil {
		return nil, err
	}
	length, err := d.readLength()
	if err != nil {
		return nil, err
	}

	items := make([]any, 0)
	if length >= 0 {
		items = make([]any, 0, length)
		for i := 0; i < length; i++ {
			value, err := d.readValue()
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		if next, ok := d.peekByte(); ok && next == 'z' {
			d.offset++
		}
	} else {
		for {
			next, ok := d.peekByte()
			if !ok {
				return nil, d.errorf("unterminated list")
			}
			if next == 'z' {
				d.offset++
				break
			}
			value, err := d.readValue()
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
	}

	if typeName == "" {
		d.refs = append(d.refs, items)
		return items, nil
	}
	list := map[string]any{
		"type":  typeName,
		"items": items,
	}
	d.refs = append(d.refs, list)
	return list, nil
}

func (d *hessianDecoder) readFixedTypedList() (any, error) {
	ref, err := d.readIntValue()
	if err != nil {
		return nil, err
	}
	if ref < 0 || ref >= len(d.types) {
		return nil, d.errorf("unknown type ref %d", ref)
	}
	length, err := d.readIntValue()
	if err != nil {
		return nil, err
	}

	items := make([]any, 0, length)
	for i := 0; i < length; i++ {
		value, err := d.readValue()
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	list := map[string]any{
		"type":  d.types[ref],
		"items": items,
	}
	d.refs = append(d.refs, list)
	return list, nil
}

func (d *hessianDecoder) readMap() (any, error) {
	typeName, err := d.readType()
	if err != nil {
		return nil, err
	}

	entries := make(map[string]any)
	if typeName == "" {
		d.refs = append(d.refs, entries)
	} else {
		d.refs = append(d.refs, map[string]any{
			"type":    typeName,
			"entries": entries,
		})
	}

	for {
		next, ok := d.peekByte()
		if !ok {
			return nil, d.errorf("unterminated map")
		}
		if next == 'z' {
			d.offset++
			break
		}
		key, err := d.readValue()
		if err != nil {
			return nil, err
		}
		value, err := d.readValue()
		if err != nil {
			return nil, err
		}
		entries[fmt.Sprint(key)] = value
	}

	if typeName == "" {
		return entries, nil
	}
	return map[string]any{
		"type":    typeName,
		"entries": entries,
	}, nil
}

func (d *hessianDecoder) readObjectDefinition() error {
	typeName, err := d.readLenString()
	if err != nil {
		return err
	}
	fieldCount, err := d.readIntValue()
	if err != nil {
		return err
	}

	fieldNames := make([]string, fieldCount)
	for i := range fieldNames {
		name, err := d.readStringValue()
		if err != nil {
			return err
		}
		fieldNames[i] = name
	}
	d.classDefs = append(d.classDefs, hessianClassDef{
		Type:       typeName,
		FieldNames: fieldNames,
	})
	return nil
}

func (d *hessianDecoder) readObjectInstance() (any, error) {
	ref, err := d.readIntValue()
	if err != nil {
		return nil, err
	}
	if ref < 0 || ref >= len(d.classDefs) {
		return nil, d.errorf("unknown class definition %d", ref)
	}

	def := d.classDefs[ref]
	fields := make(map[string]any, len(def.FieldNames))
	object := map[string]any{
		"type":       def.Type,
		"fields":     fields,
		"fieldNames": append([]string(nil), def.FieldNames...),
	}
	d.refs = append(d.refs, object)

	for _, name := range def.FieldNames {
		value, err := d.readValue()
		if err != nil {
			return nil, err
		}
		fields[name] = value
	}
	return object, nil
}

func (d *hessianDecoder) readType() (string, error) {
	tag, ok := d.peekByte()
	if !ok {
		return "", nil
	}
	switch tag {
	case 't':
		d.offset++
		hi, err := d.readByte()
		if err != nil {
			return "", err
		}
		lo, err := d.readByte()
		if err != nil {
			return "", err
		}
		typeName, err := d.readUTF8Chars(int(uint16(hi)<<8 | uint16(lo)))
		if err != nil {
			return "", err
		}
		d.types = append(d.types, typeName)
		return typeName, nil
	case 'T', hessianTypeRef:
		d.offset++
		ref, err := d.readIntValue()
		if err != nil {
			return "", err
		}
		if ref < 0 || ref >= len(d.types) {
			return "", d.errorf("unknown type ref %d", ref)
		}
		return d.types[ref], nil
	default:
		return "", nil
	}
}

func (d *hessianDecoder) readLength() (int, error) {
	tag, ok := d.peekByte()
	if !ok {
		return -1, nil
	}
	switch tag {
	case hessianLengthByte:
		d.offset++
		value, err := d.readByte()
		if err != nil {
			return -1, err
		}
		return int(value), nil
	case 'l':
		d.offset++
		return d.readInt32()
	default:
		return -1, nil
	}
}

func (d *hessianDecoder) readLenString() (string, error) {
	length, err := d.readIntValue()
	if err != nil {
		return "", err
	}
	return d.readUTF8Chars(length)
}

func (d *hessianDecoder) readStringValue() (string, error) {
	tag, err := d.readByte()
	if err != nil {
		return "", err
	}
	return d.readStringWithTag(tag)
}

func (d *hessianDecoder) readStringWithTag(tag byte) (string, error) {
	switch {
	case tag == 'N':
		return "", nil
	case tag == 'T':
		return "true", nil
	case tag == 'F':
		return "false", nil
	case tag >= 0x80 && tag <= 0xbf:
		return fmt.Sprint(int(tag) - hessianIntZero), nil
	case tag >= 0xc0 && tag <= 0xcf:
		next, err := d.readByte()
		if err != nil {
			return "", err
		}
		return fmt.Sprint((int(tag)-hessianIntByteZero)<<8 | int(next)), nil
	case tag >= 0xd0 && tag <= 0xd7:
		b1, err := d.readByte()
		if err != nil {
			return "", err
		}
		b2, err := d.readByte()
		if err != nil {
			return "", err
		}
		return fmt.Sprint((int(tag)-hessianIntShortZero)<<16 | int(b1)<<8 | int(b2)), nil
	case tag == 'I' || tag == hessianLongInt:
		value, err := d.readInt32()
		if err != nil {
			return "", err
		}
		return fmt.Sprint(value), nil
	case tag >= 0xd8 && tag <= 0xef:
		return fmt.Sprint(int(tag) - hessianLongZero), nil
	case tag >= 0xf0 && tag <= 0xff:
		next, err := d.readByte()
		if err != nil {
			return "", err
		}
		return fmt.Sprint((int(tag)-hessianLongByteZero)<<8 | int(next)), nil
	case tag >= 0x38 && tag <= 0x3f:
		b1, err := d.readByte()
		if err != nil {
			return "", err
		}
		b2, err := d.readByte()
		if err != nil {
			return "", err
		}
		return fmt.Sprint((int(tag)-hessianLongShortZero)<<16 | int(b1)<<8 | int(b2)), nil
	case tag == 'L':
		value, err := d.readInt64()
		if err != nil {
			return "", err
		}
		return fmt.Sprint(value), nil
	case tag == 'S' || tag == 's' || tag <= 0x1f:
		return d.readChunkedString(tag)
	default:
		return "", d.errorf("expected string tag, got 0x%02x", tag)
	}
}

func (d *hessianDecoder) readChunkedString(initial byte) (string, error) {
	var out []byte
	tag := initial
	for {
		var charLen int
		switch {
		case tag <= 0x1f:
			charLen = int(tag)
		case tag == 'S' || tag == 's':
			hi, err := d.readByte()
			if err != nil {
				return "", err
			}
			lo, err := d.readByte()
			if err != nil {
				return "", err
			}
			charLen = int(uint16(hi)<<8 | uint16(lo))
		default:
			return "", d.errorf("unexpected string chunk tag 0x%02x", tag)
		}

		chunk, err := d.readUTF8Bytes(charLen)
		if err != nil {
			return "", err
		}
		out = append(out, chunk...)

		if tag != 's' {
			break
		}
		next, err := d.readByte()
		if err != nil {
			return "", err
		}
		tag = next
	}
	return string(out), nil
}

func (d *hessianDecoder) readBytesWithTag(tag byte) ([]byte, error) {
	var out []byte
	current := tag
	for {
		var length int
		switch {
		case current >= 0x20 && current <= 0x2f:
			length = int(current - 0x20)
		case current == 'B' || current == 'b':
			hi, err := d.readByte()
			if err != nil {
				return nil, err
			}
			lo, err := d.readByte()
			if err != nil {
				return nil, err
			}
			length = int(uint16(hi)<<8 | uint16(lo))
		default:
			return nil, d.errorf("unexpected bytes tag 0x%02x", current)
		}

		if d.offset+length > len(d.data) {
			return nil, d.errorf("short byte slice")
		}
		out = append(out, d.data[d.offset:d.offset+length]...)
		d.offset += length

		if current != 'b' {
			break
		}
		next, err := d.readByte()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return out, nil
}

func (d *hessianDecoder) readIntValue() (int, error) {
	value, err := d.readValue()
	if err != nil {
		return 0, err
	}
	switch typed := value.(type) {
	case int64:
		return int(typed), nil
	default:
		return 0, d.errorf("expected int value, got %T", value)
	}
}

func (d *hessianDecoder) readUTF8Chars(charLen int) (string, error) {
	raw, err := d.readUTF8Bytes(charLen)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (d *hessianDecoder) readUTF8Bytes(charLen int) ([]byte, error) {
	start := d.offset
	count := 0
	for count < charLen {
		if d.offset >= len(d.data) {
			return nil, d.errorf("short utf8 string")
		}
		_, size := utf8.DecodeRune(d.data[d.offset:])
		if size == 0 {
			return nil, d.errorf("invalid utf8")
		}
		d.offset += size
		count++
	}
	return append([]byte(nil), d.data[start:d.offset]...), nil
}

func (d *hessianDecoder) readInt32() (int, error) {
	if d.offset+4 > len(d.data) {
		return 0, d.errorf("short int32")
	}
	value := int(int32(uint32(d.data[d.offset])<<24 |
		uint32(d.data[d.offset+1])<<16 |
		uint32(d.data[d.offset+2])<<8 |
		uint32(d.data[d.offset+3])))
	d.offset += 4
	return value, nil
}

func (d *hessianDecoder) readInt64() (int64, error) {
	if d.offset+8 > len(d.data) {
		return 0, d.errorf("short int64")
	}
	value := int64(uint64(d.data[d.offset])<<56 |
		uint64(d.data[d.offset+1])<<48 |
		uint64(d.data[d.offset+2])<<40 |
		uint64(d.data[d.offset+3])<<32 |
		uint64(d.data[d.offset+4])<<24 |
		uint64(d.data[d.offset+5])<<16 |
		uint64(d.data[d.offset+6])<<8 |
		uint64(d.data[d.offset+7]))
	d.offset += 8
	return value, nil
}

func (d *hessianDecoder) readByte() (byte, error) {
	if d.offset >= len(d.data) {
		return 0, d.errorf("unexpected EOF")
	}
	value := d.data[d.offset]
	d.offset++
	return value, nil
}

func (d *hessianDecoder) peekByte() (byte, bool) {
	if d.offset >= len(d.data) {
		return 0, false
	}
	return d.data[d.offset], true
}

func (d *hessianDecoder) readRef(ref int) (any, error) {
	if ref < 0 || ref >= len(d.refs) {
		return nil, d.errorf("unknown ref %d", ref)
	}
	return d.refs[ref], nil
}

func (d *hessianDecoder) errorf(format string, args ...any) error {
	return fmt.Errorf("%s at offset %d", fmt.Sprintf(format, args...), d.offset)
}
