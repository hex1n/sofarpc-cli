package invoke

import (
	"errors"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

// IsContractUnresolvable reports whether err, anywhere in its chain, is the
// contract.unresolvable rejection that lets auto contract mode fall back to
// trusted execution.
func IsContractUnresolvable(err error) bool {
	var ecerr *errcode.Error
	return errors.As(err, &ecerr) && ecerr.Code == errcode.ContractUnresolvable
}

// TrustedTupleComplete reports whether the caller supplied everything a
// trusted plan needs without a contract: service, method, parameter types,
// and args.
func TrustedTupleComplete(service, method string, paramTypes []string, args any) bool {
	return strings.TrimSpace(service) != "" &&
		strings.TrimSpace(method) != "" &&
		paramTypes != nil &&
		args != nil
}
