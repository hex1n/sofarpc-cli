package sofarpcwire

import "testing"

func FuzzDecodeResponse(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{'N'})
	if content, err := BuildSuccessResponse("ok"); err == nil {
		f.Add(content)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeResponse(data)
	})
}
