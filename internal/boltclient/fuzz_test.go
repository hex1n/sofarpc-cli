package boltclient

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func FuzzReadResponseWithLimit(f *testing.F) {
	f.Add([]byte{})
	f.Add(validResponseHeader())
	f.Add(validMinimalResponseFrame())

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadResponseWithLimit(bytes.NewReader(data), 64<<10)
	})
}

func validMinimalResponseFrame() []byte {
	raw := validResponseHeader()
	classBytes := []byte("com.alipay.sofa.rpc.core.response.SofaResponse")
	content := []byte{'N'}
	binary.BigEndian.PutUint16(raw[12:14], uint16(len(classBytes)))
	binary.BigEndian.PutUint32(raw[16:20], uint32(len(content)))
	raw = append(raw, classBytes...)
	raw = append(raw, content...)
	return raw
}
