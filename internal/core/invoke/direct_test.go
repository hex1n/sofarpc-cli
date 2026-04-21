package invoke

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/boltclient"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

const knownDirectSuccessResponseHex = "4fbe636f6d2e616c697061792e736f66612e7270632e636f72652e726573706f6e73652e536f6661526573706f6e7365940769734572726f72086572726f724d73670b617070526573706f6e73650d726573706f6e736550726f70736f90464e4fc833636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e4f7065726174696f6e526573756c7496077375636365737304636f6465076d6573736167650974696d657374616d700464617461086d657461646174616f9154e007737563636573734c0000019dae7234ef4fc847636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e726573706f6e73652e73616c65732e4461696c79486f6c64696e67526573706f6e736591116461696c79486f6c64696e67496e666f736f92566e014fc843636f6d2e746866756e642e73616c657366756e646d702e6661636164652e6d6f64656c2e726573706f6e73652e73616c65732e4461696c79486f6c64696e67496e666f94066d70436f64650866756e64436f64650b686f6c64696e67446174650f686f6c64696e675175616e746974796f934c06066c852f02200004434153480832303236303431344fa46a6176612e6d6174682e426967446563696d616c910576616c75656f9406302e303030307a4d74001e6a6176612e7574696c2e436f6c6c656374696f6e7324456d7074794d61707a4e"

func TestExecuteDirectIfPossible_UnsupportedTargetFallsThrough(t *testing.T) {
	exec, err := ExecuteDirectIfPossible(context.Background(), Plan{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Target: target.Config{
			Mode:            target.ModeRegistry,
			RegistryAddress: "zookeeper://h:1",
		},
	}, "invoke")
	if err != nil {
		t.Fatalf("ExecuteDirectIfPossible: %v", err)
	}
	if exec.Handled {
		t.Fatal("registry target should fall through to caller")
	}
}

func TestExecuteDirectIfPossible_RoundTrip(t *testing.T) {
	directURL, stop := fakeDirectServer(t, knownDirectSuccessResponseHex)
	defer stop()

	exec, err := ExecuteDirectIfPossible(context.Background(), Plan{
		Service:       "com.thfund.salesfundmp.facade.sales.holdings.SalesDailyHoldingsFacade",
		Method:        "queryPortfolioAvailableCash",
		ParamTypes:    []string{"com.thfund.salesfundmp.facade.model.request.DailyHoldingsQueryRequest"},
		Version:       "2.0",
		TargetAppName: "salesfundmp",
		Args: []any{
			map[string]any{
				"@type":      "com.thfund.salesfundmp.facade.model.request.DailyHoldingsQueryRequest",
				"tradeDate":  "20260414",
				"mpCode":     int64(434153733362950144),
				"mpCodeList": []any{int64(434153733362950144)},
			},
		},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: directURL,
		},
	}, "invoke")
	if err != nil {
		t.Fatalf("ExecuteDirectIfPossible: %v", err)
	}
	if !exec.Handled {
		t.Fatal("direct target should be handled")
	}
	if transport, _ := exec.Diagnostics["transport"].(string); transport != DirectTransportName {
		t.Fatalf("transport: got %q want %q", transport, DirectTransportName)
	}
	if got, _ := exec.Diagnostics["targetServiceUniqueName"].(string); got != "com.thfund.salesfundmp.facade.sales.holdings.SalesDailyHoldingsFacade:2.0" {
		t.Fatalf("targetServiceUniqueName: got %q", got)
	}
	if got, _ := exec.Diagnostics["requestClass"].(string); got != sofarpcwire.RequestClass {
		t.Fatalf("requestClass: got %q want %q", got, sofarpcwire.RequestClass)
	}
	result, ok := exec.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", exec.Result)
	}
	if got := result["type"]; got != "com.thfund.salesfundmp.facade.model.OperationResult" {
		t.Fatalf("result.type: got %#v", got)
	}
}

func TestExecuteDirectIfPossible_InvalidTargetReturnsErrcode(t *testing.T) {
	_, err := ExecuteDirectIfPossible(context.Background(), Plan{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://bad-target",
		},
	}, "invoke")
	if err == nil {
		t.Fatal("expected error")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.TargetUnreachable {
		t.Fatalf("code: got %q want %q", ecerr.Code, errcode.TargetUnreachable)
	}
}

func fakeDirectServer(t *testing.T, responseHex string) (string, func()) {
	t.Helper()

	content, err := hex.DecodeString(responseHex)
	if err != nil {
		t.Fatalf("decode response hex: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		requestID, err := readBoltRequestID(conn)
		if err != nil {
			return
		}
		_ = writeBoltResponse(conn, requestID, content)
	}()

	return "bolt://" + listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func readBoltRequestID(r io.Reader) (uint32, error) {
	fixed := make([]byte, 22)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return 0, err
	}
	classLen := binary.BigEndian.Uint16(fixed[14:16])
	headerLen := binary.BigEndian.Uint16(fixed[16:18])
	contentLen := binary.BigEndian.Uint32(fixed[18:22])
	body := make([]byte, int(classLen)+int(headerLen)+int(contentLen))
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(fixed[5:9]), nil
}

func writeBoltResponse(w io.Writer, requestID uint32, content []byte) error {
	classBytes := []byte(sofarpcwire.ResponseClass)
	fixed := make([]byte, 20)
	fixed[0] = boltclient.ProtocolCodeV1
	fixed[1] = boltclient.ResponseType
	binary.BigEndian.PutUint16(fixed[2:4], boltclient.CmdCodeRPCResponse)
	fixed[4] = boltclient.CmdVersion
	binary.BigEndian.PutUint32(fixed[5:9], requestID)
	fixed[9] = boltclient.CodecHessian2
	binary.BigEndian.PutUint16(fixed[10:12], 0)
	binary.BigEndian.PutUint16(fixed[12:14], uint16(len(classBytes)))
	binary.BigEndian.PutUint16(fixed[14:16], 0)
	binary.BigEndian.PutUint32(fixed[16:20], uint32(len(content)))

	if _, err := w.Write(fixed); err != nil {
		return err
	}
	if _, err := w.Write(classBytes); err != nil {
		return err
	}
	_, err := w.Write(content)
	return err
}
