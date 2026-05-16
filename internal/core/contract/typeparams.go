package contract

import (
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func typeArgBindings(typeRef string, cls javamodel.Class) map[string]string {
	if len(cls.TypeParams) == 0 {
		return nil
	}
	_, args := splitGenericArgs(strings.TrimSpace(typeRef))
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(cls.TypeParams))
	for i, param := range cls.TypeParams {
		if i >= len(args) {
			break
		}
		actual := strings.TrimSpace(args[i])
		if actual == "" {
			continue
		}
		out[param.Name] = actual
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func substituteMethodTypeParams(method javamodel.Method, bindings map[string]string) javamodel.Method {
	if len(bindings) == 0 {
		return method
	}
	out := method
	if method.ReturnTypeTemplate != "" {
		out.ReturnType = substituteTypeParams(method.ReturnTypeTemplate, bindings)
	} else {
		out.ReturnType = substituteTypeParams(method.ReturnType, bindings)
	}
	out.ParamTypes = make([]string, len(method.ParamTypes))
	for i, paramType := range method.ParamTypes {
		template := ""
		if i < len(method.ParamTypeTemplates) {
			template = method.ParamTypeTemplates[i]
		}
		if template == "" {
			template = paramType
		}
		out.ParamTypes[i] = substituteTypeParams(template, bindings)
	}
	return out
}

func substituteFieldTypeParams(field javamodel.Field, bindings map[string]string) javamodel.Field {
	if len(bindings) == 0 {
		return field
	}
	out := field
	template := field.TypeTemplate
	if template == "" {
		template = field.JavaType
	}
	out.JavaType = substituteTypeParams(template, bindings)
	return out
}

func substituteTypeParams(expr string, bindings map[string]string) string {
	if len(bindings) == 0 || strings.TrimSpace(expr) == "" {
		return expr
	}
	return formatTypeSpec(substituteTypeSpec(ParseTypeSpec(expr), bindings))
}

func substituteTypeSpec(spec TypeSpec, bindings map[string]string) TypeSpec {
	if spec.Base != "" && len(spec.Args) == 0 {
		if actual, ok := bindings[spec.Base]; ok {
			actualSpec := ParseTypeSpec(actual)
			actualSpec.ArrayDepth += spec.ArrayDepth
			if spec.Wildcard != WildcardNone {
				actualSpec.Wildcard = spec.Wildcard
			}
			return actualSpec
		}
	}
	for i := range spec.Args {
		spec.Args[i] = substituteTypeSpec(spec.Args[i], bindings)
	}
	return spec
}

func formatTypeSpec(spec TypeSpec) string {
	if spec.Wildcard == WildcardAny {
		return "?"
	}
	base := spec.Base
	if len(spec.Args) > 0 {
		args := make([]string, len(spec.Args))
		for i, arg := range spec.Args {
			args[i] = formatTypeSpec(arg)
		}
		base += "<" + strings.Join(args, ", ") + ">"
	}
	for i := 0; i < spec.ArrayDepth; i++ {
		base += "[]"
	}
	switch spec.Wildcard {
	case WildcardExtends:
		return "? extends " + base
	case WildcardSuper:
		return "? super " + base
	default:
		return base
	}
}

func typeBindingKey(fqn string, bindings map[string]string) string {
	if len(bindings) == 0 {
		return fqn
	}
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(fqn)
	b.WriteByte('<')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(bindings[key])
	}
	b.WriteByte('>')
	return b.String()
}
