package contract

import (
	"fmt"
	"sort"
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
//   - many overloads + paramTypes → exact match first, then assignable match
//     (caller type can be passed to declared type) if exact did not match.
//     Non-unique matches return contract.method-ambiguous.
//
// paramTypes are expected to be fully qualified names (e.g. "java.lang.String").
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

	overloads := collectOverloads(store, cls, method)
	if len(overloads) == 0 {
		return Result{}, errcode.New(errcode.MethodNotFound, "contract",
			fmt.Sprintf("no method %q on %s", method, service)).
			WithHint("sofarpc_describe", map[string]any{"service": service}, "list available methods")
	}

	selected, err := pickOverload(store, service, method, overloads, paramTypes)
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

// ListMethods returns every method callable on a service, including methods
// inherited from superinterfaces and superclasses with type parameters
// substituted. Results are deduplicated by signature and sorted by name then
// signature so listings are stable across runs.
func ListMethods(store Store, service string) (javamodel.Class, []javamodel.Method, error) {
	if strings.TrimSpace(service) == "" {
		return javamodel.Class{}, nil, errcode.New(errcode.ServiceMissing, "contract", "service is required").
			WithHint("sofarpc_describe", nil, "call describe without service to list known services")
	}
	cls, ok := store.Class(service)
	if !ok {
		return javamodel.Class{}, nil, errcode.New(errcode.ContractUnresolvable, "contract",
			fmt.Sprintf("service %q is not in the facade index", service)).
			WithHint("sofarpc_doctor", nil, "facade index may be missing or stale")
	}
	methods := collectMethods(store, cls, func(string) bool { return true })
	sort.SliceStable(methods, func(i, j int) bool {
		if methods[i].Name != methods[j].Name {
			return methods[i].Name < methods[j].Name
		}
		return methodSignature(methods[i]) < methodSignature(methods[j])
	})
	return cls, methods, nil
}

func collectOverloads(store Store, cls javamodel.Class, name string) []javamodel.Method {
	return collectMethods(store, cls, func(candidate string) bool { return candidate == name })
}

func collectMethods(store Store, cls javamodel.Class, match func(name string) bool) []javamodel.Method {
	var out []javamodel.Method
	seen := map[string]bool{}
	seenSignatures := map[string]bool{}

	var walk func(javamodel.Class, map[string]string)
	walk = func(current javamodel.Class, bindings map[string]string) {
		seenKey := typeBindingKey(current.FQN, bindings)
		if current.FQN == "" || seen[seenKey] {
			return
		}
		seen[seenKey] = true
		for _, m := range current.Methods {
			m = substituteMethodTypeParams(m, bindings)
			if match(m.Name) {
				signature := methodSignature(m)
				if seenSignatures[signature] {
					continue
				}
				seenSignatures[signature] = true
				out = append(out, m)
			}
		}
		for _, ifaceName := range current.Interfaces {
			ifaceRef := substituteTypeParams(ifaceName, bindings)
			if iface, ok := store.Class(rawJavaTypeName(ifaceRef)); ok {
				walk(iface, typeArgBindings(ifaceRef, iface))
			}
		}
		if current.Superclass != "" {
			superRef := substituteTypeParams(current.Superclass, bindings)
			if super, ok := store.Class(rawJavaTypeName(superRef)); ok {
				walk(super, typeArgBindings(superRef, super))
			}
		}
	}

	walk(cls, nil)
	return out
}

func methodSignature(m javamodel.Method) string {
	return m.Name + "(" + strings.Join(m.ParamTypes, ",") + ")"
}

func pickOverload(store Store, service, method string, overloads []javamodel.Method, paramTypes []string) (int, error) {
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
	if len(matches) == 0 {
		for i, m := range overloads {
			if paramTypesAssignable(store, paramTypes, m.ParamTypes) {
				matches = append(matches, i)
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return 0, errcode.New(errcode.MethodNotFound, "contract",
			fmt.Sprintf("no overload of %s.%s matches or accepts paramTypes %v", service, method, paramTypes)).
			WithHint("sofarpc_describe", map[string]any{"service": service, "method": method}, "inspect declared overloads")
	default:
		return 0, ambiguousErr(service, method, overloads, "paramTypes matched or were assignable to more than one overload")
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

func paramTypesAssignable(store Store, actual, declared []string) bool {
	if len(actual) != len(declared) {
		return false
	}
	for i := range actual {
		if !paramTypeAssignable(store, actual[i], declared[i]) {
			return false
		}
	}
	return true
}

func paramTypeAssignable(store Store, actual, declared string) bool {
	actualSpec := ParseTypeSpec(actual)
	declaredSpec := ParseTypeSpec(declared)
	if actualSpec.IsZero() || declaredSpec.IsZero() {
		return false
	}
	if actualSpec.ArrayDepth != declaredSpec.ArrayDepth {
		return false
	}
	if actualSpec.Base == declaredSpec.Base {
		return true
	}
	if declaredSpec.Base == "java.lang.Object" {
		return true
	}
	return typeAssignable(store, actualSpec.Base, declaredSpec.Base)
}
