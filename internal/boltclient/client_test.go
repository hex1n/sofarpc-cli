package boltclient

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncodeDecodeSimpleMap(t *testing.T) {
	t.Parallel()

	encoded, err := EncodeSimpleMap(map[string]string{
		"service": "com.example.DemoFacade:1.0",
		"type":    "sync",
	})
	if err != nil {
		t.Fatalf("EncodeSimpleMap() error = %v", err)
	}

	decoded, err := DecodeSimpleMap(encoded)
	if err != nil {
		t.Fatalf("DecodeSimpleMap() error = %v", err)
	}
	if decoded["service"] != "com.example.DemoFacade:1.0" {
		t.Fatalf("decoded service = %q", decoded["service"])
	}
	if decoded["type"] != "sync" {
		t.Fatalf("decoded type = %q", decoded["type"])
	}
}

func TestReadResponse(t *testing.T) {
	t.Parallel()

	header, err := EncodeSimpleMap(map[string]string{"sofa_head_generic_type": "2"})
	if err != nil {
		t.Fatalf("EncodeSimpleMap() error = %v", err)
	}

	classBytes := []byte("com.alipay.sofa.rpc.core.response.SofaResponse")
	contentBytes := []byte{0x4e}

	raw := make([]byte, responseHeaderLengthV1+len(classBytes)+len(header)+len(contentBytes))
	raw[0] = ProtocolCodeV1
	raw[1] = ResponseType
	binary.BigEndian.PutUint16(raw[2:4], CmdCodeRPCResponse)
	raw[4] = CmdVersion
	binary.BigEndian.PutUint32(raw[5:9], 42)
	raw[9] = CodecHessian2
	binary.BigEndian.PutUint16(raw[10:12], 0)
	binary.BigEndian.PutUint16(raw[12:14], uint16(len(classBytes)))
	binary.BigEndian.PutUint16(raw[14:16], uint16(len(header)))
	binary.BigEndian.PutUint32(raw[16:20], uint32(len(contentBytes)))
	offset := responseHeaderLengthV1
	copy(raw[offset:], classBytes)
	offset += len(classBytes)
	copy(raw[offset:], header)
	offset += len(header)
	copy(raw[offset:], contentBytes)

	resp, err := ReadResponse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	if resp.RequestID != 42 {
		t.Fatalf("RequestID = %d", resp.RequestID)
	}
	if resp.ResponseClass != string(classBytes) {
		t.Fatalf("ResponseClass = %q", resp.ResponseClass)
	}
	if resp.Header["sofa_head_generic_type"] != "2" {
		t.Fatalf("generic header = %q", resp.Header["sofa_head_generic_type"])
	}
}

// TestEncodeRequestFrameLayout pins the BOLT v1 request frame layout:
// fixed prefix (protocol, type, cmd, cmd-version, request-id, codec,
// timeout, class-len, header-len, content-len), followed by class name,
// serialized header, then content verbatim. This replaces an older
// test that compared against a golden hex captured from a Java client;
// the structural assertions here catch the same encoder drifts without
// pinning any specific facade name.
func TestEncodeRequestFrameLayout(t *testing.T) {
	t.Parallel()

	const (
		requestClass = "com.alipay.sofa.rpc.core.request.SofaRequest"
		serviceUID   = "com.example.demo.ExampleFacade:1.0"
		methodName   = "query"
	)
	headers := map[string]string{
		"service":                  serviceUID,
		"sofa_head_method_name":    methodName,
		"sofa_head_target_service": serviceUID,
		"sofa_head_generic_type":   "2",
		"type":                     "sync",
		"generic.revise":           "true",
	}
	content := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	frame, err := EncodeRequest(Request{
		RequestID:    1,
		RequestClass: requestClass,
		Header:       headers,
		Content:      content,
		Codec:        CodecHessian2,
		Timeout:      10_000_000_000,
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}

	if got := frame[0]; got != ProtocolCodeV1 {
		t.Fatalf("protocol code = 0x%02x", got)
	}
	if got := frame[1]; got != RequestType {
		t.Fatalf("frame type = 0x%02x", got)
	}
	if got := binary.BigEndian.Uint16(frame[2:4]); got != CmdCodeRPCRequest {
		t.Fatalf("cmd code = %d", got)
	}
	if got := frame[4]; got != CmdVersion {
		t.Fatalf("cmd version = %d", got)
	}
	if got := binary.BigEndian.Uint32(frame[5:9]); got != 1 {
		t.Fatalf("request id = %d", got)
	}
	if got := frame[9]; got != CodecHessian2 {
		t.Fatalf("codec = 0x%02x", got)
	}
	if got := binary.BigEndian.Uint32(frame[10:14]); got != 10_000 {
		t.Fatalf("timeout ms = %d", got)
	}

	classLen := int(binary.BigEndian.Uint16(frame[14:16]))
	headerLen := int(binary.BigEndian.Uint16(frame[16:18]))
	contentLen := int(binary.BigEndian.Uint32(frame[18:22]))
	if classLen != len(requestClass) {
		t.Fatalf("class length = %d want %d", classLen, len(requestClass))
	}
	if contentLen != len(content) {
		t.Fatalf("content length = %d want %d", contentLen, len(content))
	}
	if want := requestHeaderLengthV1 + classLen + headerLen + contentLen; len(frame) != want {
		t.Fatalf("frame length = %d want %d", len(frame), want)
	}

	classStart := requestHeaderLengthV1
	classEnd := classStart + classLen
	if got := string(frame[classStart:classEnd]); got != requestClass {
		t.Fatalf("request class = %q", got)
	}

	headerStart := classEnd
	headerEnd := headerStart + headerLen
	decodedHeader, err := DecodeSimpleMap(frame[headerStart:headerEnd])
	if err != nil {
		t.Fatalf("DecodeSimpleMap() error = %v", err)
	}
	for key, want := range headers {
		if got := decodedHeader[key]; got != want {
			t.Fatalf("header[%q] = %q want %q", key, got, want)
		}
	}

	if got := frame[headerEnd:]; !bytes.Equal(got, content) {
		t.Fatalf("content bytes = % x want % x", got, content)
	}
}
