package boltclient

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
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

func TestEncodeRequestMatchesJavaFrame(t *testing.T) {
	t.Parallel()

	const wantHex = "0101000101000000010100002710002c014200000237636f6d2e616c697061792e736f66612e7270632e636f72652e726571756573742e536f666152657175657374000000077365727669636500000049636f6d2e746866756e642e73616c657366756e646d702e6661636164652e73616c65732e686f6c64696e67732e53616c65734461696c79486f6c64696e67734661636164653a312e3000000015736f66615f686561645f6d6574686f645f6e616d650000001b7175657279506f7274666f6c696f417661696c61626c654361736800000018736f66615f686561645f7461726765745f7365727669636500000049636f6d2e746866756e642e73616c657366756e646d702e6661636164652e73616c65732e686f6c64696e67732e53616c65734461696c79486f6c64696e67734661636164653a312e3000000016736f66615f686561645f67656e657269635f74797065000000013200000004747970650000000473796e630000000e67656e657269632e72657669736500000004747275654fbc636f6d2e616c697061792e736f66612e7270632e636f72652e726571756573742e536f666152657175657374950d7461726765744170704e616d650a6d6574686f644e616d651774617267657453657276696365556e697175654e616d650c7265717565737450726f70730d6d6574686f64417267536967736f904e1b7175657279506f7274666f6c696f417661696c61626c6543617368530049636f6d2e746866756e642e73616c657366756e646d702e6661636164652e73616c65732e686f6c64696e67732e53616c65734461696c79486f6c64696e67734661636164653a312e304d16736f66615f686561645f67656e657269635f7479706501320e67656e657269632e726576697365047472756504747970650473796e637a567400075b737472696e676e01530045636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e726571756573742e4461696c79486f6c64696e67735175657279526571756573747a4d7400176a6176612e7574696c2e4c696e6b6564486173684d6170054074797065530045636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e726571756573742e4461696c79486f6c64696e677351756572795265717565737409747261646544617465083230323630343134066d70436f64654c06066c852f0220000a6d70436f64654c6973745674001a6a6176612e7574696c2e4172726179732441727261794c6973746e014c06066c852f0220007a7a"
	content, err := hex.DecodeString("4fbc636f6d2e616c697061792e736f66612e7270632e636f72652e726571756573742e536f666152657175657374950d7461726765744170704e616d650a6d6574686f644e616d651774617267657453657276696365556e697175654e616d650c7265717565737450726f70730d6d6574686f64417267536967736f904e1b7175657279506f7274666f6c696f417661696c61626c6543617368530049636f6d2e746866756e642e73616c657366756e646d702e6661636164652e73616c65732e686f6c64696e67732e53616c65734461696c79486f6c64696e67734661636164653a312e304d16736f66615f686561645f67656e657269635f7479706501320e67656e657269632e726576697365047472756504747970650473796e637a567400075b737472696e676e01530045636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e726571756573742e4461696c79486f6c64696e67735175657279526571756573747a4d7400176a6176612e7574696c2e4c696e6b6564486173684d6170054074797065530045636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e726571756573742e4461696c79486f6c64696e677351756572795265717565737409747261646544617465083230323630343134066d70436f64654c06066c852f0220000a6d70436f64654c6973745674001a6a6176612e7574696c2e4172726179732441727261794c6973746e014c06066c852f0220007a7a")
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	frame, err := EncodeRequest(Request{
		RequestID:    1,
		RequestClass: "com.alipay.sofa.rpc.core.request.SofaRequest",
		Header: map[string]string{
			"service":                  "com.thfund.salesfundmp.facade.sales.holdings.SalesDailyHoldingsFacade:1.0",
			"sofa_head_method_name":    "queryPortfolioAvailableCash",
			"sofa_head_target_service": "com.thfund.salesfundmp.facade.sales.holdings.SalesDailyHoldingsFacade:1.0",
			"sofa_head_generic_type":   "2",
			"type":                     "sync",
			"generic.revise":           "true",
		},
		Content: content,
		Codec:   CodecHessian2,
		Timeout: 10_000_000_000,
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	gotHex := hex.EncodeToString(frame)
	if gotHex != wantHex {
		t.Fatalf("frame mismatch\nwant=%s\ngot =%s", wantHex, gotHex)
	}
}
