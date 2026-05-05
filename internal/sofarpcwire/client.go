package sofarpcwire

import (
	"context"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
)

type DirectInvokeOptions struct {
	Addr             string
	Codec            byte
	Timeout          time.Duration
	RequestID        uint32
	MaxResponseBytes int64
}

type DirectInvokeResult struct {
	RequestID uint32
	Request   EncodedRequest
	Response  boltclient.Response
	Decoded   *DecodedResponse
	DecodeErr error
}

func InvokeDirect(ctx context.Context, spec RequestSpec, opts DirectInvokeOptions) (DirectInvokeResult, error) {
	encoded, err := BuildGenericRequest(spec)
	if err != nil {
		return DirectInvokeResult{}, err
	}
	resp, err := boltclient.Invoke(ctx, opts.Addr, boltclient.Request{
		RequestID:        opts.RequestID,
		RequestClass:     encoded.Class,
		Header:           encoded.Header,
		Content:          encoded.Content,
		Codec:            opts.Codec,
		Timeout:          opts.Timeout,
		MaxResponseBytes: opts.MaxResponseBytes,
	})
	if err != nil {
		return DirectInvokeResult{}, err
	}

	result := DirectInvokeResult{
		RequestID: opts.RequestID,
		Request:   encoded,
		Response:  resp,
	}
	if len(resp.Content) > 0 {
		decoded, err := DecodeResponse(resp.Content)
		if err != nil {
			result.DecodeErr = err
			return result, nil
		}
		result.Decoded = &decoded
	}
	return result, nil
}
