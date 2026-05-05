package sourcecontract

import (
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

// parsedClass is the pre-materialise shape of a parsed Java class. It
// carries the surface extracted from source (raw type strings, raw
// superclass, ...) together with the resolution tables needed to turn
// those strings into FQNs.
type parsedClass struct {
	path          string
	packageName   string
	fqn           string
	simpleName    string
	typeParams    map[string]string
	kind          string
	superclass    string
	interfaces    []string
	fields        []parsedField
	methods       []parsedMethod
	enumConstants []string
	imports       importTable
	localTypes    map[string]string
	projectTypes  symbolTable
}

type parsedField struct {
	name     string
	javaType string
}

type parsedMethod struct {
	name       string
	paramTypes []string
	returnType string
}

// importTable is the pre-resolved view of a file's import list.
// explicit maps simple-name → FQN for `import a.b.C;` lines; wildcards
// carries the prefix for `import a.b.*;` lines.
type importTable struct {
	explicit  map[string]string
	wildcards []string
}

func materializeClass(src parsedClass) (javamodel.Class, []typeResolutionIssue) {
	cls := javamodel.Class{
		FQN:        src.fqn,
		SimpleName: src.simpleName,
		File:       src.path,
		Kind:       src.kind,
	}
	var issues []typeResolutionIssue
	resolver := typeResolver{
		pkg:        src.packageName,
		classFQN:   src.fqn,
		explicit:   src.imports.explicit,
		wildcards:  src.imports.wildcards,
		localTypes: src.localTypes,
		project:    src.projectTypes,
		typeParams: src.typeParams,
		issues:     &issues,
	}
	if src.superclass != "" {
		cls.Superclass = resolver.resolve(src.superclass)
	}
	for _, iface := range src.interfaces {
		cls.Interfaces = append(cls.Interfaces, resolver.resolve(iface))
	}
	for _, field := range src.fields {
		cls.Fields = append(cls.Fields, javamodel.Field{
			Name:     field.name,
			JavaType: resolver.resolve(field.javaType),
		})
	}
	for _, method := range src.methods {
		m := javamodel.Method{
			Name:       method.name,
			ReturnType: resolver.resolve(method.returnType),
		}
		for _, pt := range method.paramTypes {
			m.ParamTypes = append(m.ParamTypes, resolver.resolve(pt))
		}
		cls.Methods = append(cls.Methods, m)
	}
	cls.EnumConstants = append(cls.EnumConstants, src.enumConstants...)
	return cls, issues
}

func materializeClasses(path, pkg string, imports importTable, decl declaration, projectSymbols symbolTable) ([]javamodel.Class, []typeResolutionIssue) {
	parsed := flattenDeclarations(path, pkg, imports, decl, nil, nil, nil, projectSymbols)
	if len(parsed) == 0 {
		return nil, nil
	}
	out := make([]javamodel.Class, 0, len(parsed))
	var issues []typeResolutionIssue
	for _, item := range parsed {
		cls, clsIssues := materializeClass(item)
		out = append(out, cls)
		issues = append(issues, clsIssues...)
	}
	return out, issues
}

// flattenDeclarations walks the nested declaration tree and emits one
// parsedClass per declaration, threading enclosing type names into the
// FQN so inner classes render as Outer.Inner.
func flattenDeclarations(path, pkg string, imports importTable, decl declaration, outers []string, visible map[string]string, visibleTypeParams map[string]string, projectSymbols symbolTable) []parsedClass {
	currentOuters := append(append([]string(nil), outers...), decl.simpleName)
	currentFQN := joinFQN(pkg, currentOuters)
	locals := cloneVisibleMap(visible)
	locals[decl.simpleName] = currentFQN
	for _, child := range decl.nested {
		locals[child.simpleName] = joinFQN(pkg, append(currentOuters, child.simpleName))
	}
	typeParams := cloneVisibleMap(visibleTypeParams)
	for name, bound := range decl.typeParams {
		typeParams[name] = bound
	}

	current := parsedClass{
		path:          path,
		packageName:   pkg,
		fqn:           currentFQN,
		simpleName:    decl.simpleName,
		typeParams:    cloneVisibleMap(typeParams),
		kind:          decl.kind,
		superclass:    decl.superclass,
		interfaces:    append([]string(nil), decl.interfaces...),
		fields:        append([]parsedField(nil), decl.fields...),
		methods:       append([]parsedMethod(nil), decl.methods...),
		enumConstants: append([]string(nil), decl.enumConstants...),
		imports:       imports,
		localTypes:    locals,
		projectTypes:  projectSymbols,
	}

	out := []parsedClass{current}
	for _, child := range decl.nested {
		out = append(out, flattenDeclarations(path, pkg, imports, child, currentOuters, locals, typeParams, projectSymbols)...)
	}
	return out
}

func joinFQN(pkg string, names []string) string {
	name := strings.Join(names, ".")
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

func cloneVisibleMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneStringSliceMap(input map[string][]string) map[string][]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string][]string, len(input))
	for key, value := range input {
		out[key] = append([]string(nil), value...)
	}
	return out
}

func cloneIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]int, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
