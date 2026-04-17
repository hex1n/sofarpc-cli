package facadeschema

import "github.com/hex1n/sofarpc-cli/internal/facadesemantic"

type SemanticIndex = facadesemantic.SemanticIndex
type SemanticClassInfo = facadesemantic.SemanticClassInfo
type SemanticFieldInfo = facadesemantic.SemanticFieldInfo
type SemanticMethodInfo = facadesemantic.SemanticMethodInfo
type SemanticParameterInfo = facadesemantic.SemanticParameterInfo
type Registry = facadesemantic.Registry

var LoadSemanticRegistry = facadesemantic.LoadSemanticRegistry
var IsFacadeInterface = facadesemantic.IsFacadeInterface
