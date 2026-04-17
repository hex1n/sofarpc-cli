package contract

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/projectscan"
)

func TestBuildServiceSchemaConvertsImportedAndSamePackageTypes(t *testing.T) {
	registry := facadesemantic.Registry{
		"com.example.OrderFacade": {
			FQN:           "com.example.OrderFacade",
			Kind:          "interface",
			SamePkgPrefix: "com.example",
			Imports: map[string]string{
				"List": "java.util.List",
			},
			Methods: []facadesemantic.SemanticMethodInfo{
				{
					Name:       "importAsset",
					ReturnType: "List<OrderResult>",
					Parameters: []facadesemantic.SemanticParameterInfo{
						{Name: "request", Type: "OrderRequest"},
						{Name: "items", Type: "List<OrderItem>"},
						{Name: "tags", Type: "java.lang.String[]"},
					},
				},
			},
		},
		"com.example.OrderRequest": {FQN: "com.example.OrderRequest", Kind: "class"},
		"com.example.OrderItem":    {FQN: "com.example.OrderItem", Kind: "class"},
		"com.example.OrderResult":  {FQN: "com.example.OrderResult", Kind: "class"},
	}

	schema, err := BuildServiceSchema(registry, "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("BuildServiceSchema() error = %v", err)
	}
	if len(schema.Methods) != 1 {
		t.Fatalf("Methods len = %d", len(schema.Methods))
	}
	method := schema.Methods[0]
	if !reflect.DeepEqual(method.ParamTypes, []string{
		"com.example.OrderRequest",
		"java.util.List",
		"java.lang.String[]",
	}) {
		t.Fatalf("ParamTypes = %v", method.ParamTypes)
	}
	if !reflect.DeepEqual(method.ParamTypeSignatures, []string{
		"OrderRequest",
		"List<OrderItem>",
		"java.lang.String[]",
	}) {
		t.Fatalf("ParamTypeSignatures = %v", method.ParamTypeSignatures)
	}
	if method.ReturnType != "java.util.List" {
		t.Fatalf("ReturnType = %q", method.ReturnType)
	}
}

func TestBuildServiceSchemaSortsOverloads(t *testing.T) {
	registry := facadesemantic.Registry{
		"com.example.UserFacade": {
			FQN:  "com.example.UserFacade",
			Kind: "interface",
			Methods: []facadesemantic.SemanticMethodInfo{
				{Name: "find", Parameters: []facadesemantic.SemanticParameterInfo{{Name: "id", Type: "java.lang.Long"}}},
				{Name: "create", Parameters: []facadesemantic.SemanticParameterInfo{{Name: "req", Type: "com.example.CreateRequest"}}},
				{Name: "find", Parameters: []facadesemantic.SemanticParameterInfo{{Name: "name", Type: "java.lang.String"}}},
			},
		},
	}

	schema, err := BuildServiceSchema(registry, "com.example.UserFacade")
	if err != nil {
		t.Fatalf("BuildServiceSchema() error = %v", err)
	}
	got := []string{
		schema.Methods[0].Name + ":" + schema.Methods[0].ParamTypeSignatures[0],
		schema.Methods[1].Name + ":" + schema.Methods[1].ParamTypeSignatures[0],
		schema.Methods[2].Name + ":" + schema.Methods[2].ParamTypeSignatures[0],
	}
	want := []string{
		"create:com.example.CreateRequest",
		"find:java.lang.Long",
		"find:java.lang.String",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method order = %v, want %v", got, want)
	}
}

func TestDescribeServiceFromProjectNarrowsMatchedModule(t *testing.T) {
	originalDiscover := discoverProjectFn
	originalMatch := matchServiceFn
	originalLoadConfig := loadProjectConfigFn
	originalLoadRegistry := loadSemanticRegistryFn
	t.Cleanup(func() {
		discoverProjectFn = originalDiscover
		matchServiceFn = originalMatch
		loadProjectConfigFn = originalLoadConfig
		loadSemanticRegistryFn = originalLoadRegistry
	})

	discoverProjectFn = func(startDir string) (projectscan.ProjectLayout, error) {
		return projectscan.ProjectLayout{
			Root: startDir,
			FacadeModules: []projectscan.FacadeModule{
				{Name: "order-facade", SourceRoot: "order-facade/src/main/java", MavenModulePath: "order-facade"},
				{Name: "user-facade", SourceRoot: "user-facade/src/main/java", MavenModulePath: "user-facade"},
			},
		}, nil
	}
	matchServiceFn = func(projectRoot, serviceFQCN string, modules []projectscan.FacadeModule) (projectscan.ServiceMatch, error) {
		return projectscan.ServiceMatch{Module: modules[1]}, nil
	}
	loadProjectConfigFn = func(string, bool) (facadeconfig.Config, error) {
		return facadeconfig.DefaultConfig(), nil
	}
	loadSemanticRegistryFn = func(projectRoot string, sourceRoots, markers []string) (facadesemantic.Registry, error) {
		want := []string{filepath.Clean(filepath.Join(projectRoot, "user-facade", "src", "main", "java"))}
		if !reflect.DeepEqual(sourceRoots, want) {
			t.Fatalf("sourceRoots = %v, want %v", sourceRoots, want)
		}
		return facadesemantic.Registry{
			"com.example.UserFacade": {
				FQN:  "com.example.UserFacade",
				Kind: "interface",
				Methods: []facadesemantic.SemanticMethodInfo{
					{Name: "getUser"},
				},
			},
		}, nil
	}

	schema, err := DescribeServiceFromProject(t.TempDir(), "com.example.UserFacade")
	if err != nil {
		t.Fatalf("DescribeServiceFromProject() error = %v", err)
	}
	if schema.Service != "com.example.UserFacade" || len(schema.Methods) != 1 {
		t.Fatalf("schema = %+v", schema)
	}
}

func TestDescribeServiceFromProjectFallsBackToAllModulesWhenMatchFails(t *testing.T) {
	originalDiscover := discoverProjectFn
	originalMatch := matchServiceFn
	originalLoadConfig := loadProjectConfigFn
	originalLoadRegistry := loadSemanticRegistryFn
	t.Cleanup(func() {
		discoverProjectFn = originalDiscover
		matchServiceFn = originalMatch
		loadProjectConfigFn = originalLoadConfig
		loadSemanticRegistryFn = originalLoadRegistry
	})

	discoverProjectFn = func(startDir string) (projectscan.ProjectLayout, error) {
		return projectscan.ProjectLayout{
			Root: startDir,
			FacadeModules: []projectscan.FacadeModule{
				{Name: "order-facade", SourceRoot: "order-facade/src/main/java"},
				{Name: "user-facade", SourceRoot: "user-facade/src/main/java"},
			},
		}, nil
	}
	matchServiceFn = func(string, string, []projectscan.FacadeModule) (projectscan.ServiceMatch, error) {
		return projectscan.ServiceMatch{}, errors.New("not found")
	}
	loadProjectConfigFn = func(string, bool) (facadeconfig.Config, error) {
		return facadeconfig.DefaultConfig(), nil
	}
	loadSemanticRegistryFn = func(projectRoot string, sourceRoots, markers []string) (facadesemantic.Registry, error) {
		want := []string{
			filepath.Clean(filepath.Join(projectRoot, "order-facade", "src", "main", "java")),
			filepath.Clean(filepath.Join(projectRoot, "user-facade", "src", "main", "java")),
		}
		if !reflect.DeepEqual(sourceRoots, want) {
			t.Fatalf("sourceRoots = %v, want %v", sourceRoots, want)
		}
		return facadesemantic.Registry{
			"com.example.OrderFacade": {FQN: "com.example.OrderFacade", Kind: "interface"},
		}, nil
	}

	if _, err := DescribeServiceFromProject(t.TempDir(), "com.example.OrderFacade"); err != nil {
		t.Fatalf("DescribeServiceFromProject() error = %v", err)
	}
}
