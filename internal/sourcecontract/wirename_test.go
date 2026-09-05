package sourcecontract

import (
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
)

// Materialized classes carry both identities: FQN stays source-canonical
// (the Store's index key), BinaryName carries the JVM '$' form Hessian2
// needs. Top-level classes have BinaryName == FQN.
func TestLoad_PopulatesBinaryName(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Outer.java", `
package com.foo;
public class Outer {
    private Inner current;

    public static class Inner {
        public static class Deep {
            private String name;
        }
    }
}
`)
	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := map[string]string{
		"com.foo.Outer":            "com.foo.Outer",
		"com.foo.Outer.Inner":      "com.foo.Outer$Inner",
		"com.foo.Outer.Inner.Deep": "com.foo.Outer$Inner$Deep",
	}
	for fqn, want := range cases {
		cls, ok := store.Class(fqn)
		if !ok {
			t.Fatalf("%s not found", fqn)
		}
		if cls.BinaryName != want {
			t.Errorf("%s BinaryName: got %q want %q", fqn, cls.BinaryName, want)
		}
	}
}

// An enclosing class may legally carry a literal '$' in its own name.
// The nested binary name then mixes literal and separator dollars
// (Dollar$Request$Inner ↔ canonical Dollar$Request.Inner), and explicit
// binary @type values must still resolve to the indexed class.
func TestNestedClassInsideLiteralDollarOuter(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/foo/Dollar$Request.java", `
package com.foo;
public class Dollar$Request {
    private Inner inner;

    public static class Inner {
        private String name;
    }
}
`)
	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	inner, ok := store.Class("com.foo.Dollar$Request.Inner")
	if !ok {
		t.Fatal("Inner not found by canonical name")
	}
	if inner.BinaryName != "com.foo.Dollar$Request$Inner" {
		t.Fatalf("Inner BinaryName: got %q", inner.BinaryName)
	}

	args := []any{map[string]any{
		"@type": "com.foo.Dollar$Request$Inner",
		"name":  "n1",
	}}
	out, err := contract.NormalizeArgs([]string{"com.foo.Dollar$Request.Inner"}, args, store)
	if err != nil {
		t.Fatalf("NormalizeArgs rejected a valid nested binary name: %v", err)
	}
	obj := out[0].(map[string]any)
	if got := obj["@type"]; got != "com.foo.Dollar$Request$Inner" {
		t.Fatalf("@type: got %v want mixed literal/separator binary name", got)
	}
}

// Nested static classes must reach the Hessian2 wire as JVM binary names
// (Outer$Inner): the server resolves @type via Class.forName, which does
// not accept the source-canonical dot form the contract store indexes by.
func TestNestedDTO_WireTypeNameIsBinaryName(t *testing.T) {
	root := t.TempDir()
	writeJava(t, root, "src/main/java/com/example/UpsertSeasonTimeWindowRequest.java", `
package com.example;
import java.util.List;
import java.util.Map;
public class UpsertSeasonTimeWindowRequest {
    private String seasonId;
    private List<CustomTimeWindow> customTimeWindows;
    private CustomTimeWindow primaryWindow;
    private CustomTimeWindow[] windowArray;
    private Map<String, CustomTimeWindow> windowByName;
    private CustomTimeWindow.Boundary deepBoundary;

    public static class CustomTimeWindow {
        private String beginDate;

        public static class Boundary {
            private String edge;
        }
    }
}
`)
	store, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	paramTypes := []string{"com.example.UpsertSeasonTimeWindowRequest"}
	// Args as an agent would send them after copying the describe skeleton:
	// explicit @type in the list uses the canonical dot form; the other
	// nested fields rely on the declared type alone.
	args := []any{map[string]any{
		"@type":    "com.example.UpsertSeasonTimeWindowRequest",
		"seasonId": "2026Q3",
		"customTimeWindows": []any{
			map[string]any{
				"@type":     "com.example.UpsertSeasonTimeWindowRequest.CustomTimeWindow",
				"beginDate": "20260701",
			},
		},
		"primaryWindow": map[string]any{"beginDate": "20260702"},
		"windowArray":   []any{map[string]any{"beginDate": "20260703"}},
		"windowByName":  map[string]any{"first": map[string]any{"beginDate": "20260704"}},
		"deepBoundary":  map[string]any{"edge": "E1"},
	}}

	normalized, err := contract.NormalizeArgs(paramTypes, args, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	encoded, err := sofarpcwire.BuildGenericRequest(sofarpcwire.RequestSpec{
		Service:    "com.example.SeasonBgFacade",
		Method:     "upsertSeasonTimeWindow",
		ParamTypes: paramTypes,
		Args:       normalized,
	})
	if err != nil {
		t.Fatalf("BuildGenericRequest: %v", err)
	}

	content := string(encoded.Content)
	binary := "com.example.UpsertSeasonTimeWindowRequest$CustomTimeWindow"
	if !strings.Contains(content, binary) {
		t.Errorf("wire content is missing binary nested type name %q", binary)
	}
	if !strings.Contains(content, binary+"$Boundary") {
		t.Errorf("wire content is missing multi-level binary type name %q", binary+"$Boundary")
	}
	if strings.Contains(content, "UpsertSeasonTimeWindowRequest.CustomTimeWindow") {
		t.Errorf("canonical dot-form nested type name leaked to the wire")
	}
}
