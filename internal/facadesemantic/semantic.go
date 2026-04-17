package facadesemantic

import "strings"

type SemanticIndex struct {
	Classes []SemanticClassInfo `json:"classes"`
}

type SemanticClassInfo struct {
	FQN           string               `json:"fqn"`
	SimpleName    string               `json:"simple_name"`
	File          string               `json:"file"`
	Kind          string               `json:"kind"`
	Superclass    string               `json:"superclass,omitempty"`
	Imports       map[string]string    `json:"imports,omitempty"`
	SamePkgPrefix string               `json:"same_pkg_prefix,omitempty"`
	Fields        []SemanticFieldInfo  `json:"fields,omitempty"`
	EnumConstants []string             `json:"enum_constants,omitempty"`
	Methods       []SemanticMethodInfo `json:"methods,omitempty"`
	MethodReturns []string             `json:"method_returns,omitempty"`
}

type SemanticFieldInfo struct {
	Name     string `json:"name"`
	JavaType string `json:"java_type"`
	Comment  string `json:"comment,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type SemanticMethodInfo struct {
	Name       string                  `json:"name"`
	Javadoc    string                  `json:"javadoc,omitempty"`
	ReturnType string                  `json:"returnType,omitempty"`
	Parameters []SemanticParameterInfo `json:"parameters,omitempty"`
}

type SemanticParameterInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Registry map[string]SemanticClassInfo

func IsFacadeInterface(classInfo SemanticClassInfo, suffixes []string) bool {
	if classInfo.Kind != "interface" {
		return false
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(classInfo.SimpleName, suffix) {
			return true
		}
	}
	return false
}
