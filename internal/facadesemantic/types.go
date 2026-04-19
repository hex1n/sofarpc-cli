// Package facadesemantic holds the shapes mirrored by the Java indexer's
// shard output. The indexer writes these as JSON under .sofarpc/index/,
// the Go side reads them back verbatim. Field names, tags, and zero-value
// semantics MUST match docs/architecture.md §6.2.
//
// This package has no logic — it is types only. Callers that need to
// reason about a Class use internal/core/contract or internal/javatype.
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

// Field is one declared member. Required is indexer-supplied (e.g. from a
// @NotNull / primitive declaration) and purely advisory here.
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

// Kind constants mirror what the indexer emits. Kept as string literals
// so reading raw shard JSON is obvious.
const (
	KindClass     = "class"
	KindInterface = "interface"
	KindEnum      = "enum"
)
