// Package sofarpcwire implements the BOLT v1 + Hessian v1 wire
// encoding/decoding for SOFARPC generic invoke.
//
// File layout for the decoder side:
//   - decoder.go — Hessian decoder struct, top-level tag dispatcher, list/map/object readers, type/length/ref helpers.
//   - decoder_primitive.go — low-level primitive readers (bytes, ints, UTF-8, chunked strings).
//   - response.go — Sofa-layer adapters that flatten the generic decoded value into the DecodedResponse shape.
package sofarpcwire

import (
	"fmt"
	"math"
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

// hessianClassDef records one typed object definition seen earlier in
// the stream. The decoder replays it whenever a later instance tag
// references the same class by index.
type hessianClassDef struct {
	Type       string
	FieldNames []string
}

// hessianDecoder is a minimal Hessian v1 reader tuned for the SOFARPC
// response envelope. It maintains three tables as required by the spec:
// a ref table for already-decoded values, a class-def table for typed
// objects, and a type-ref table for shared type strings.
type hessianDecoder struct {
	data      []byte
	offset    int
	refs      []any
	classDefs []hessianClassDef
	types     []string
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

func (d *hessianDecoder) readRef(ref int) (any, error) {
	if ref < 0 || ref >= len(d.refs) {
		return nil, d.errorf("unknown ref %d", ref)
	}
	return d.refs[ref], nil
}
