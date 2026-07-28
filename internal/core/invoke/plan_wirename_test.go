package invoke

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
)

func writeJavaSource(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// Plan.ParamTypes feed SofaRequest.methodArgSigs verbatim, and the server
// loads each signature with Class.forName — so nested classes must appear
// in JVM binary form there too, including inside generic arguments.
func TestBuildPlan_ParamTypeSignaturesUseBinaryNestedTypeNames(t *testing.T) {
	root := t.TempDir()
	writeJavaSource(t, root, "src/main/java/com/example/SeasonBgFacade.java", `
package com.example;
import java.util.List;
public interface SeasonBgFacade {
    String upsertWindow(UpsertSeasonTimeWindowRequest.CustomTimeWindow window, List<UpsertSeasonTimeWindowRequest.CustomTimeWindow> windows);
}
`)
	writeJavaSource(t, root, "src/main/java/com/example/UpsertSeasonTimeWindowRequest.java", `
package com.example;
public class UpsertSeasonTimeWindowRequest {
    public static class CustomTimeWindow {
        private String beginDate;
    }
}
`)
	store, err := sourcecontract.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	plan, err := BuildPlan(
		Input{
			Service: "com.example.SeasonBgFacade",
			Method:  "upsertWindow",
			Target:  target.Input{DirectURL: "bolt://host:12200"},
		},
		store,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	binary := "com.example.UpsertSeasonTimeWindowRequest$CustomTimeWindow"
	want := []string{binary, "java.util.List<" + binary + ">"}
	if len(plan.ParamTypes) != len(want) {
		t.Fatalf("paramTypes arity: got %v", plan.ParamTypes)
	}
	for i, w := range want {
		if plan.ParamTypes[i] != w {
			t.Errorf("paramTypes[%d]: got %q want %q", i, plan.ParamTypes[i], w)
		}
	}
}

// The skeleton-as-args branch of resolveArgs returns the decoded skeleton
// without passing through NormalizeArgs, so it must rewrite nested-class
// @type values to JVM binary names on its own. A source-contract store is
// used because only it knows a class's binary name.
func TestBuildPlan_SkeletonArgsUseBinaryNestedTypeNames(t *testing.T) {
	root := t.TempDir()
	writeJavaSource(t, root, "src/main/java/com/example/SeasonBgFacade.java", `
package com.example;
public interface SeasonBgFacade {
    String upsertSeasonTimeWindow(UpsertSeasonTimeWindowRequest request);
}
`)
	writeJavaSource(t, root, "src/main/java/com/example/UpsertSeasonTimeWindowRequest.java", `
package com.example;
import java.util.List;
public class UpsertSeasonTimeWindowRequest {
    private List<CustomTimeWindow> customTimeWindows;

    public static class CustomTimeWindow {
        private String beginDate;
    }
}
`)
	store, err := sourcecontract.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	plan, err := BuildPlan(
		Input{
			Service: "com.example.SeasonBgFacade",
			Method:  "upsertSeasonTimeWindow",
			Target:  target.Input{DirectURL: "bolt://host:12200"},
		},
		store,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.ArgSource != "skeleton" {
		t.Fatalf("argSource: got %q want skeleton", plan.ArgSource)
	}
	arg, ok := plan.Args[0].(map[string]any)
	if !ok {
		t.Fatalf("skeleton arg should be an object, got %T", plan.Args[0])
	}
	windows, ok := arg["customTimeWindows"].([]any)
	if !ok || len(windows) == 0 {
		t.Fatalf("customTimeWindows skeleton missing: %v", arg)
	}
	window, ok := windows[0].(map[string]any)
	if !ok {
		t.Fatalf("customTimeWindows[0] should be an object, got %T", windows[0])
	}
	want := "com.example.UpsertSeasonTimeWindowRequest$CustomTimeWindow"
	if got := window["@type"]; got != want {
		t.Fatalf("nested skeleton @type: got %v want %s", got, want)
	}
}
