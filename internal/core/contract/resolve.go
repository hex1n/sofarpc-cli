package contract

import (
	"fmt"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// Result is what ResolveMethod returns on success. Overloads lists every
// method with the matching name (useful for describe output); Selected
// is the index into Overloads of the disambiguated overload.
type Result struct {
	Service   javamodel.Class
	Method    javamodel.Method
	Overloads []javamodel.Method
	Selected  int
}

// ResolveMethod looks up a method on a service and disambiguates overloads.
//
// Rules:
//   - service and method are required; empty values return *errcode.Error
//     with input.service-missing / input.method-missing.
//   - service not in store → contract.unresolvable with hint to doctor.
//   - no method with name → contract.method-not-found.
//   - one overload → returned directly, Selected=0.
//   - many overloads + no paramTypes → contract.method-ambiguous.
//   - many overloads + paramTypes → exact match (arity + element equality)
//     or contract.method-ambiguous if the types don't uniquely pick one.
//
// paramTypes are matched case-sensitively against Method.ParamTypes. The
// caller is expected to pass fully qualified names (e.g. "java.lang.String").
func ResolveMethod(store Store, service, method string, paramTypes []string) (Result, error) {
	if strings.TrimSpace(service) == "" {
		return Result{}, errcode.New(errcode.ServiceMissing, "contract", "service is required").
			WithHint("sofarpc_open", nil, "open a workspace to list known services")
	}
	if strings.TrimSpace(method) == "" {
		return Result{}, errcode.New(errcode.MethodMissing, "contract", "method is required").
			WithHint("sofarpc_describe", map[string]any{"service": service}, "describe the service to see its methods")
	}
	cls, ok := store.Class(service)
	if !ok {
		return Result{}, errcode.New(errcode.ContractUnresolvable, "contract",
			fmt.Sprintf("service %q is not in the facade index", service)).
			WithHint("sofarpc_doctor", nil, "facade index may be missing or stale")
	}

	overloads := collectOverloads(cls, method)
	if len(overloads) == 0 {
		return Result{}, errcode.New(errcode.MethodNotFound, "contract",
			fmt.Sprintf("no method %q on %s", method, service)).
			WithHint("sofarpc_describe", map[string]any{"service": service}, "list available methods")
	}

	selected, err := pickOverload(service, method, overloads, paramTypes)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Service:   cls,
		Method:    overloads[selected],
		Overloads: overloads,
		Selected:  selected,
	}, nil
}

func collectOverloads(cls javamodel.Class, name string) []javamodel.Method {
	var out []javamodel.Method
	for _, m := range cls.Methods {
		if m.Name == name {
			out = append(out, m)
		}
	}
	return out
}

func pickOverload(service, method string, overloads []javamodel.Method, paramTypes []string) (int, error) {
	if len(overloads) == 1 {
		return 0, nil
	}
	if len(paramTypes) == 0 {
		return 0, ambiguousErr(service, method, overloads, "supply paramTypes to disambiguate")
	}
	matches := make([]int, 0, len(overloads))
	for i, m := range overloads {
		if paramTypesEqual(m.ParamTypes, paramTypes) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return 0, errcode.New(errcode.MethodNotFound, "contract",
			fmt.Sprintf("no overload of %s.%s matches paramTypes %v", service, method, paramTypes)).
			WithHint("sofarpc_describe", map[string]any{"service": service, "method": method}, "inspect declared overloads")
	default:
		return 0, ambiguousErr(service, method, overloads, "paramTypes matched more than one overload")
	}
}

func ambiguousErr(service, method string, overloads []javamodel.Method, reason string) error {
	return errcode.New(errcode.MethodAmbiguous, "contract",
		fmt.Sprintf("method %q on %s has %d overloads", method, service, len(overloads))).
		WithHint("sofarpc_describe", map[string]any{"service": service, "method": method}, reason)
}

func paramTypesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
