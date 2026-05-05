package sofarpcwire

import (
	"fmt"
	"unicode/utf8"
)

// decoder_primitive.go holds the low-level byte/string readers used by
// the Hessian decoder. They do not know anything about the SOFARPC
// response envelope — every function here maps directly onto a primitive
// tag class in the Hessian v1 grammar.

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
		if err := d.checkScalarByteLength("string", len(out)+len(chunk)); err != nil {
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
		if err := d.checkScalarByteLength("bytes", len(out)+length); err != nil {
			return nil, err
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
	if charLen < 0 {
		return nil, d.errorf("utf8 string length %d is invalid", charLen)
	}
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
	raw := append([]byte(nil), d.data[start:d.offset]...)
	if err := d.checkScalarByteLength("string", len(raw)); err != nil {
		return nil, err
	}
	return raw, nil
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

func (d *hessianDecoder) errorf(format string, args ...any) error {
	return fmt.Errorf("%s at offset %d", fmt.Sprintf(format, args...), d.offset)
}
