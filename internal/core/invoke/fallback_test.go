package invoke

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func TestIsContractUnresolvable(t *testing.T) {
	unresolvable := errcode.New(errcode.ContractUnresolvable, "invoke", "service is not in the contract index")
	if !IsContractUnresolvable(unresolvable) {
		t.Fatal("contract.unresolvable should be recognised")
	}
	if !IsContractUnresolvable(fmt.Errorf("build plan: %w", unresolvable)) {
		t.Fatal("a wrapped contract.unresolvable should be recognised")
	}
	if IsContractUnresolvable(errcode.New(errcode.ArgsInvalid, "invoke", "bad args")) {
		t.Fatal("another errcode must not trigger the trusted fallback")
	}
	if IsContractUnresolvable(errors.New("plain failure")) {
		t.Fatal("a plain error must not trigger the trusted fallback")
	}
	if IsContractUnresolvable(nil) {
		t.Fatal("nil must not trigger the trusted fallback")
	}
}

func TestTrustedTupleComplete(t *testing.T) {
	types := []string{"java.lang.String"}
	args := []any{"x"}
	cases := []struct {
		name    string
		service string
		method  string
		types   []string
		args    any
		want    bool
	}{
		{"complete tuple", "com.foo.Facade", "query", types, args, true},
		{"missing service", "", "query", types, args, false},
		{"blank method", "com.foo.Facade", "   ", types, args, false},
		{"no param types", "com.foo.Facade", "query", nil, args, false},
		{"nil args", "com.foo.Facade", "query", types, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrustedTupleComplete(tc.service, tc.method, tc.types, tc.args); got != tc.want {
				t.Fatalf("TrustedTupleComplete(%q, %q, %v, %v) = %v, want %v", tc.service, tc.method, tc.types, tc.args, got, tc.want)
			}
		})
	}
}
