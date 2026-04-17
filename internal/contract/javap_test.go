package contract

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/projectscan"
)

func TestParseJavapClassInterfaceMethods(t *testing.T) {
	output := `Compiled from "OrderFacade.java"
public interface com.example.OrderFacade {
  public abstract com.example.Result importAsset(com.example.OrderRequest);
  public abstract java.util.List<com.example.Item> find(java.util.List<com.example.Query>);
}`
	info, err := parseJavapClass(output, "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("parseJavapClass() error = %v", err)
	}
	if info.Kind != "interface" || info.FQN != "com.example.OrderFacade" {
		t.Fatalf("info = %+v", info)
	}
	if len(info.Methods) != 2 {
		t.Fatalf("Methods len = %d", len(info.Methods))
	}
	if info.Methods[1].ReturnType != "java.util.List<com.example.Item>" {
		t.Fatalf("ReturnType = %q", info.Methods[1].ReturnType)
	}
}

func TestParseJavapClassPrivateFields(t *testing.T) {
	output := `Compiled from "OrderRequest.java"
public class com.example.OrderRequest extends com.example.BaseRequest {
  private java.util.List<com.example.Item> items;
  transient java.util.Map<java.lang.String, com.example.Meta> meta;
  public static final com.example.Status ACTIVE;
  public com.example.OrderRequest();
}`
	info, err := parseJavapClass(output, "com.example.OrderRequest")
	if err != nil {
		t.Fatalf("parseJavapClass() error = %v", err)
	}
	if info.Kind != "class" || info.Superclass != "com.example.BaseRequest" {
		t.Fatalf("info = %+v", info)
	}
	if len(info.Fields) != 2 {
		t.Fatalf("Fields = %+v", info.Fields)
	}
}

func TestResolveMethodFromArtifactsBuildsRegistryFromJavap(t *testing.T) {
	origDiscoverProject := discoverProjectFn
	origDiscoverArtifacts := discoverArtifactsFn
	origMatchService := matchServiceFn
	origRunJavap := runJavap
	t.Cleanup(func() {
		discoverProjectFn = origDiscoverProject
		discoverArtifactsFn = origDiscoverArtifacts
		matchServiceFn = origMatchService
		runJavap = origRunJavap
	})

	discoverProjectFn = func(startDir string) (projectscan.ProjectLayout, error) {
		return projectscan.ProjectLayout{
			Root: startDir,
			FacadeModules: []projectscan.FacadeModule{
				{Name: "order-facade", MavenModulePath: "order-facade"},
			},
		}, nil
	}
	discoverArtifactsFn = func(projectRoot string, module projectscan.FacadeModule) (projectscan.ArtifactSet, error) {
		return projectscan.ArtifactSet{
			PrimaryJars:    []string{"order-facade.jar"},
			DependencyJars: []string{"deps.jar"},
		}, nil
	}
	matchServiceFn = func(projectRoot, serviceFQCN string, modules []projectscan.FacadeModule) (projectscan.ServiceMatch, error) {
		return projectscan.ServiceMatch{Module: modules[0]}, nil
	}
	runJavap = func(classpath []string, includeFields bool, fqcn string) (string, error) {
		if !reflect.DeepEqual(classpath, []string{"order-facade.jar", "deps.jar"}) {
			t.Fatalf("classpath = %v", classpath)
		}
		switch fqcn {
		case "com.example.OrderFacade":
			return `public interface com.example.OrderFacade {
  public abstract com.example.Result importAsset(com.example.OrderRequest);
}`, nil
		case "com.example.OrderRequest":
			return `public class com.example.OrderRequest {
  private java.util.List<com.example.Item> items;
  private com.example.Child child;
}`, nil
		case "com.example.Item":
			return `public class com.example.Item {
  private java.lang.String code;
}`, nil
		case "com.example.Child":
			return `public class com.example.Child {
  private java.lang.String name;
}`, nil
		case "com.example.Result":
			return `public class com.example.Result {
  private java.lang.String status;
}`, nil
		default:
			return "", nil
		}
	}

	method, err := ResolveMethodFromArtifacts(t.TempDir(), "com.example.OrderFacade", "importAsset", nil, json.RawMessage(`[{"items":[{"code":"A1"}],"child":{"name":"kid"}}]`))
	if err != nil {
		t.Fatalf("ResolveMethodFromArtifacts() error = %v", err)
	}
	if len(method.Registry) < 4 {
		t.Fatalf("Registry keys = %v", mapsKeys(method.Registry))
	}
	compiled, err := CompileProjectMethodArgs(json.RawMessage(`[{"items":[{"code":"A1"}],"child":{"name":"kid"}}]`), method)
	if err != nil {
		t.Fatalf("CompileProjectMethodArgs() error = %v", err)
	}
	text := string(compiled)
	for _, fragment := range []string{
		`"@type":"com.example.OrderRequest"`,
		`"@type":"com.example.Item"`,
		`"@type":"com.example.Child"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("compiled = %s, missing %s", text, fragment)
		}
	}
}

func TestDescribeServiceFromArtifactsBuildsSchema(t *testing.T) {
	origDiscoverProject := discoverProjectFn
	origDiscoverArtifacts := discoverArtifactsFn
	origMatchService := matchServiceFn
	origRunJavap := runJavap
	t.Cleanup(func() {
		discoverProjectFn = origDiscoverProject
		discoverArtifactsFn = origDiscoverArtifacts
		matchServiceFn = origMatchService
		runJavap = origRunJavap
	})

	discoverProjectFn = func(startDir string) (projectscan.ProjectLayout, error) {
		return projectscan.ProjectLayout{
			Root: startDir,
			FacadeModules: []projectscan.FacadeModule{
				{Name: "order-facade", MavenModulePath: "order-facade"},
			},
		}, nil
	}
	discoverArtifactsFn = func(projectRoot string, module projectscan.FacadeModule) (projectscan.ArtifactSet, error) {
		return projectscan.ArtifactSet{PrimaryJars: []string{"order-facade.jar"}}, nil
	}
	matchServiceFn = func(projectRoot, serviceFQCN string, modules []projectscan.FacadeModule) (projectscan.ServiceMatch, error) {
		return projectscan.ServiceMatch{Module: modules[0]}, nil
	}
	runJavap = func(classpath []string, includeFields bool, fqcn string) (string, error) {
		return `public interface com.example.OrderFacade {
  public abstract com.example.Result importAsset(com.example.OrderRequest);
}`, nil
	}

	schema, err := DescribeServiceFromArtifacts("C:/tmp", "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("DescribeServiceFromArtifacts() error = %v", err)
	}
	if !reflect.DeepEqual(schema, model.ServiceSchema{
		Service: "com.example.OrderFacade",
		Methods: []model.MethodSchema{{Name: "importAsset", ParamTypes: []string{"com.example.OrderRequest"}, ParamTypeSignatures: []string{"com.example.OrderRequest"}, ReturnType: "com.example.Result"}},
	}) {
		t.Fatalf("schema = %+v", schema)
	}
}

func mapsKeys(registry map[string]facadesemantic.SemanticClassInfo) []string {
	keys := make([]string, 0, len(registry))
	for key := range registry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
