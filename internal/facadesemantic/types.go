// Package facadesemantic holds the contract shapes used by the pure-Go
// contract resolver. It is types only: callers that need to reason about a
// Class use internal/core/contract or internal/javatype.
//
// Field names and zero-value semantics are kept explicit so these shapes can
// be materialised from any contract source without leaking source-specific
// behavior into the resolver.
package facadesemantic

// Class is one top-level Java type. Kind distinguishes class/interface/enum
// so downstream code can skip enums during method resolution and walk
// enumConstants for describe-time rendering.
type Class struct {
	FQN           string   `json:"fqn"`
	SimpleName    string   `json:"simpleName"`
	File          string   `json:"file,omitempty"`
	Kind          string   `json:"kind"`
	Superclass    string   `json:"superclass,omitempty"`
	Interfaces    []string `json:"interfaces,omitempty"`
	Fields        []Field  `json:"fields,omitempty"`
	EnumConstants []string `json:"enumConstants,omitempty"`
	Methods       []Method `json:"methods,omitempty"`
}

// Field is one declared member. Required is purely advisory here.
type Field struct {
	Name     string `json:"name"`
	JavaType string `json:"javaType"`
	Required bool   `json:"required,omitempty"`
}

// Method is one declared method. Overloads share a Name — the resolver
// disambiguates by ParamTypes length + exact match when the agent supplies
// types, and errors with MethodAmbiguous otherwise.
type Method struct {
	Name       string   `json:"name"`
	ParamTypes []string `json:"paramTypes,omitempty"`
	ReturnType string   `json:"returnType,omitempty"`
}

// Kind constants stay as string literals so they are easy to inspect in tests
// and any external contract materialisation.
const (
	KindClass     = "class"
	KindInterface = "interface"
	KindEnum      = "enum"
)
