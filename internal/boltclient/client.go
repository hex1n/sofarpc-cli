package boltclient

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sort"
	"time"
)

const (
	ProtocolCodeV1 byte = 1

	RequestType  byte = 1
	ResponseType byte = 0

	CmdCodeRPCRequest  uint16 = 1
	CmdCodeRPCResponse uint16 = 2
	CmdVersion         byte   = 1

	// BOLT wire codec byte for Hessian payloads. This is distinct from
	// SOFARPC's higher-level serializer constants such as
	// SERIALIZE_CODE_HESSIAN2 = 4.
	CodecHessian2 byte = 1

	requestHeaderLengthV1  = 22
	responseHeaderLengthV1 = 20

	// DefaultMaxResponseBytes caps the BOLT response body before allocating
	// class/header/content buffers. It covers the variable-length body only;
	// the fixed 20-byte response header is read separately.
	DefaultMaxResponseBytes int64 = 16 << 20
)

type Request struct {
	RequestID    uint32
	RequestClass string
	Header       map[string]string
	Content      []byte
	Codec        byte
	Timeout      time.Duration

	// MaxResponseBytes caps the variable-length response body. A value <= 0
	// uses DefaultMaxResponseBytes.
	MaxResponseBytes int64
}

type Response struct {
	ProtocolCode   byte
	Type           byte
	CmdCode        uint16
	CmdVersion     byte
	RequestID      uint32
	Codec          byte
	ResponseStatus uint16
	ResponseClass  string
	Header         map[string]string
	Content        []byte
	Raw            []byte
}

func Invoke(ctx context.Context, addr string, req Request) (Response, error) {
	frame, err := EncodeRequest(req)
	if err != nil {
		return Response{}, err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Response{}, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return Response{}, fmt.Errorf("set deadline: %w", err)
		}
	} else if req.Timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(req.Timeout)); err != nil {
			return Response{}, fmt.Errorf("set deadline: %w", err)
		}
	}

	if _, err := conn.Write(frame); err != nil {
		return Response{}, fmt.Errorf("write request frame: %w", err)
	}

	resp, err := ReadResponseWithLimit(conn, req.MaxResponseBytes)
	if err != nil {
		return Response{}, err
	}
	if resp.RequestID != req.RequestID {
		return Response{}, fmt.Errorf("response requestId mismatch: got=%d want=%d", resp.RequestID, req.RequestID)
	}
	return resp, nil
}

func EncodeRequest(req Request) ([]byte, error) {
	if req.RequestID == 0 {
		return nil, fmt.Errorf("requestId is required")
	}
	if req.RequestClass == "" {
		return nil, fmt.Errorf("requestClass is required")
	}

	codec := req.Codec
	if codec == 0 {
		codec = CodecHessian2
	}

	classBytes := []byte(req.RequestClass)
	headerBytes, err := EncodeSimpleMap(req.Header)
	if err != nil {
		return nil, fmt.Errorf("encode header: %w", err)
	}
	contentBytes := req.Content
	timeoutMs := durationMilliseconds(req.Timeout)

	frame := make([]byte, requestHeaderLengthV1+len(classBytes)+len(headerBytes)+len(contentBytes))
	frame[0] = ProtocolCodeV1
	frame[1] = RequestType
	binary.BigEndian.PutUint16(frame[2:4], CmdCodeRPCRequest)
	frame[4] = CmdVersion
	binary.BigEndian.PutUint32(frame[5:9], req.RequestID)
	frame[9] = codec
	binary.BigEndian.PutUint32(frame[10:14], timeoutMs)
	binary.BigEndian.PutUint16(frame[14:16], uint16(len(classBytes)))
	binary.BigEndian.PutUint16(frame[16:18], uint16(len(headerBytes)))
	binary.BigEndian.PutUint32(frame[18:22], uint32(len(contentBytes)))

	offset := requestHeaderLengthV1
	copy(frame[offset:], classBytes)
	offset += len(classBytes)
	copy(frame[offset:], headerBytes)
	offset += len(headerBytes)
	copy(frame[offset:], contentBytes)
	return frame, nil
}

func ReadResponse(r io.Reader) (Response, error) {
	return ReadResponseWithLimit(r, DefaultMaxResponseBytes)
}

func ReadResponseWithLimit(r io.Reader, maxBodyBytes int64) (Response, error) {
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultMaxResponseBytes
	}

	fixed := make([]byte, responseHeaderLengthV1)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return Response{}, fmt.Errorf("read response header: %w", err)
	}
	if fixed[0] != ProtocolCodeV1 {
		return Response{}, fmt.Errorf("unsupported protocol code %d", fixed[0])
	}
	if fixed[1] != ResponseType {
		return Response{}, fmt.Errorf("unexpected response frame type %d", fixed[1])
	}
	if cmdCode := binary.BigEndian.Uint16(fixed[2:4]); cmdCode != CmdCodeRPCResponse {
		return Response{}, fmt.Errorf("unexpected response command code %d", cmdCode)
	}
	if fixed[4] != CmdVersion {
		return Response{}, fmt.Errorf("unexpected response command version %d", fixed[4])
	}
	if fixed[9] != CodecHessian2 {
		return Response{}, fmt.Errorf("unexpected response codec %d", fixed[9])
	}

	classLen := binary.BigEndian.Uint16(fixed[12:14])
	headerLen := binary.BigEndian.Uint16(fixed[14:16])
	contentLen := binary.BigEndian.Uint32(fixed[16:20])
	bodyLen := uint64(classLen) + uint64(headerLen) + uint64(contentLen)
	if bodyLen > uint64(maxBodyBytes) {
		return Response{}, fmt.Errorf("response body length %d exceeds limit %d", bodyLen, maxBodyBytes)
	}
	body := make([]byte, int(bodyLen))
	if _, err := io.ReadFull(r, body); err != nil {
		return Response{}, fmt.Errorf("read response body: %w", err)
	}

	classEnd := int(classLen)
	headerEnd := classEnd + int(headerLen)
	header, err := DecodeSimpleMap(body[classEnd:headerEnd])
	if err != nil {
		return Response{}, fmt.Errorf("decode response header: %w", err)
	}

	raw := append(append([]byte(nil), fixed...), body...)
	return Response{
		ProtocolCode:   fixed[0],
		Type:           fixed[1],
		CmdCode:        binary.BigEndian.Uint16(fixed[2:4]),
		CmdVersion:     fixed[4],
		RequestID:      binary.BigEndian.Uint32(fixed[5:9]),
		Codec:          fixed[9],
		ResponseStatus: binary.BigEndian.Uint16(fixed[10:12]),
		ResponseClass:  string(body[:classEnd]),
		Header:         header,
		Content:        append([]byte(nil), body[headerEnd:]...),
		Raw:            raw,
	}, nil
}

func EncodeSimpleMap(values map[string]string) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}

	buf := make([]byte, 0, len(values)*16)
	for _, key := range orderedSimpleMapKeys(values) {
		value := values[key]
		buf = appendSizedString(buf, key)
		buf = appendSizedString(buf, value)
	}
	return buf, nil
}

func DecodeSimpleMap(data []byte) (map[string]string, error) {
	if len(data) == 0 {
		return map[string]string{}, nil
	}

	out := make(map[string]string)
	for offset := 0; offset < len(data); {
		key, next, err := readSizedString(data, offset)
		if err != nil {
			return nil, err
		}
		value, nextValue, err := readSizedString(data, next)
		if err != nil {
			return nil, err
		}
		out[key] = value
		offset = nextValue
	}
	return out, nil
}

func appendSizedString(dst []byte, value string) []byte {
	size := int32(len(value))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(size))
	dst = append(dst, length[:]...)
	dst = append(dst, value...)
	return dst
}

func readSizedString(data []byte, offset int) (string, int, error) {
	if len(data[offset:]) < 4 {
		return "", offset, fmt.Errorf("short simple-map length at offset %d", offset)
	}
	size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if len(data[offset:]) < size {
		return "", offset, fmt.Errorf("short simple-map string at offset %d", offset)
	}
	value := string(data[offset : offset+size])
	return value, offset + size, nil
}

func durationMilliseconds(timeout time.Duration) uint32 {
	if timeout <= 0 {
		return 0
	}
	return uint32(timeout / time.Millisecond)
}

func orderedSimpleMapKeys(values map[string]string) []string {
	known := []string{
		"service",
		"sofa_head_method_name",
		"sofa_head_target_service",
		"sofa_head_generic_type",
		"type",
		"generic.revise",
	}
	if len(values) == len(known) {
		match := true
		for _, key := range known {
			if _, ok := values[key]; !ok {
				match = false
				break
			}
		}
		if match {
			return known
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
