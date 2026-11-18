package contract

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func nestedWindowStore() *InMemoryStore {
	return NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.example.UpsertReq",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "customTimeWindows", JavaType: "java.util.List<com.example.UpsertReq.CustomTimeWindow>"},
			},
		},
		javamodel.Class{
			FQN:        "com.example.UpsertReq.CustomTimeWindow",
			BinaryName: "com.example.UpsertReq$CustomTimeWindow",
			Kind:       javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "beginDate", JavaType: "java.lang.String"},
			},
		},
		javamodel.Class{
			FQN:  "com.example.Unrelated",
			Kind: javamodel.KindClass,
		},
	)
}

// An explicit @type carrying the JVM binary spelling (Outer$Inner) names
// the same class as the declared canonical type (Outer.Inner) and must be
// accepted — and still be emitted in wire (binary) form.
func TestNormalizeArgs_AcceptsExplicitBinaryNestedTypeName(t *testing.T) {
	store := nestedWindowStore()
	args := []any{map[string]any{
		"customTimeWindows": []any{
			map[string]any{
				"@type":     "com.example.UpsertReq$CustomTimeWindow",
				"beginDate": "20260701",
			},
		},
	}}
	out, err := NormalizeArgs([]string{"com.example.UpsertReq"}, args, store)
	if err != nil {
		t.Fatalf("NormalizeArgs rejected explicit binary name: %v", err)
	}
	req := out[0].(map[string]any)
	windows := req["customTimeWindows"].([]any)
	window := windows[0].(map[string]any)
	if got := window["@type"]; got != "com.example.UpsertReq$CustomTimeWindow" {
		t.Fatalf("window @type: got %v want binary name", got)
	}
	if got := window["beginDate"]; got != "20260701" {
		t.Fatalf("beginDate: got %v", got)
	}
}

// A literal '$' is legal in Java identifiers: com.foo.Dollar$Request can
// be a top-level class, not a nested one. Name resolution must prefer the
// exact spelling the Store knows before falling back to the canonical
// dot form, or declared fields degrade to loose handling.
func TestNormalizeArgs_PreservesLiteralDollarTopLevelClass(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Dollar$Request",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "amount", JavaType: "java.math.BigDecimal"},
			},
		},
	)
	args := []any{map[string]any{"amount": "12.50"}}
	out, err := NormalizeArgs([]string{"com.foo.Dollar$Request"}, args, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}
	req := out[0].(map[string]any)
	if got := req["@type"]; got != "com.foo.Dollar$Request" {
		t.Fatalf("@type: got %v want literal-dollar FQN", got)
	}
	amount, ok := req["amount"].(map[string]any)
	if !ok {
		t.Fatalf("amount lost declared BigDecimal normalization: got %#v (%T)", req["amount"], req["amount"])
	}
	if got := amount["@type"]; got != "java.math.BigDecimal" {
		t.Fatalf("amount @type: got %v", got)
	}
	if got := amount["value"]; got != "12.50" {
		t.Fatalf("amount value: got %v", got)
	}
}

func TestWireParamTypes_ErasesGenericArraysAndPreservesNestedBinaryNames(t *testing.T) {
	store := NewInMemoryStore(javamodel.Class{
		FQN: "com.foo.Outer.Inner", BinaryName: "com.foo.Outer$Inner", Kind: javamodel.KindClass,
	})
	got := WireParamTypes([]string{"java.util.List<com.foo.Outer.Inner>[]", "com.foo.Outer.Inner[]", "java.util.List<"}, store)
	if got[0] != "java.util.List[]" || got[1] != "com.foo.Outer$Inner[]" || got[2] != "java.util.List<" {
		t.Fatalf("wire signatures: %q", got)
	}
}

func TestWireParamTypes_BudgetsUnresolvableDollarTokens(t *testing.T) {
	store := NewInMemoryStore()
	token := strings.Repeat("a$", 64) + "z"
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(token)
	}
	expr := sb.String()

	out := WireParamTypes([]string{expr}, store)
	if out[0] != expr {
		t.Fatal("fully unresolvable expression must come back unchanged")
	}
	allocs := testing.AllocsPerRun(5, func() {
		WireParamTypes([]string{expr}, store)
	})
	if allocs > 1024 {
		t.Fatalf("1000 bounded-dollar tokens cost %v allocs per call; fallback resolution needs a per-rewrite budget", allocs)
	}
}

// The fallback budget must cover the WHOLE WireParamTypes call: a fresh
// budget per parameter expression lets the same flood come back spread
// across a thousand one-token expressions.
func TestWireParamTypes_SharesBudgetAcrossExpressions(t *testing.T) {
	store := NewInMemoryStore()
	params := make([]string, 1000)
	for i := range params {
		params[i] = fmt.Sprintf("p%d.%sz", i, strings.Repeat("a$", 64))
	}
	out := WireParamTypes(params, store)
	for i := range params {
		if out[i] != params[i] {
			t.Fatalf("unresolvable param %d must come back unchanged", i)
		}
	}
	allocs := testing.AllocsPerRun(5, func() {
		WireParamTypes(params, store)
	})
	if allocs > 2048 {
		t.Fatalf("1000 bounded-dollar param types cost %v allocs per call; the budget must span the whole call", allocs)
	}
}

// NormalizeArgs walks caller-supplied trees too: loose objects with
// unresolvable '$'-heavy @type values must charge one shared per-call
// budget instead of restarting the per-name budget on every lookup.
func TestNormalizeArgs_BoundsUnresolvableDollarTypeFallback(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{FQN: "com.example.Holder", Kind: javamodel.KindClass},
	)
	items := make([]any, 1000)
	for i := range items {
		items[i] = map[string]any{
			"@type": fmt.Sprintf("p%d.%sz", i, strings.Repeat("a$", 64)),
			"v":     "x",
		}
	}
	args := []any{items}
	if _, err := NormalizeArgs([]string{"java.lang.Object"}, args, store); err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}
	allocs := testing.AllocsPerRun(3, func() {
		if _, err := NormalizeArgs([]string{"java.lang.Object"}, args, store); err != nil {
			t.Fatalf("NormalizeArgs: %v", err)
		}
	})
	if allocs > 16384 {
		t.Fatalf("1000 dollar-heavy loose objects cost %v allocs per call; NormalizeArgs needs a per-call resolution budget", allocs)
	}
}

// An unresolvable caller-supplied name with thousands of '$' separators
// must not cost one full-length candidate string per separator: MCP and
// replay type strings are attacker-controlled, and quadratic allocation
// at JVM-valid name lengths turns them into a resource sink.
func TestStoreTypeName_BoundsUnresolvableDollarChains(t *testing.T) {
	store := NewInMemoryStore()
	name := "com.foo." + strings.Repeat("A$", 5000) + "End"
	allocs := testing.AllocsPerRun(10, func() {
		if got := storeTypeName(name, store); got != name {
			t.Fatalf("unresolvable name must be returned unchanged, got %q", got)
		}
	})
	if allocs > 16 {
		t.Fatalf("unresolvable 5000-separator chain cost %v allocs per call; resolution must be bounded", allocs)
	}
}

// Dollar-vs-dot equivalence is a Store-verified fact, not a spelling rule:
// when neither spelling resolves, the two names may be two genuinely
// different legal classes and must NOT be equated — that would bypass
// assignability validation entirely.
func TestNormalizeArgs_UnknownDollarClassIsNotAssumedNested(t *testing.T) {
	store := NewInMemoryStore()
	args := []any{map[string]any{
		"@type":  "com.foo.Dollar$Request",
		"amount": "1",
	}}
	_, err := NormalizeArgs([]string{"com.foo.Dollar.Request"}, args, store)
	if err == nil {
		t.Fatal("unknown literal-dollar top-level class was treated as the unknown dotted class")
	}
	if !strings.Contains(err.Error(), "not assignable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Accepting binary spellings must not loosen the check itself: a genuinely
// unrelated explicit @type is still rejected, echoing the caller's input.
func TestNormalizeArgs_StillRejectsUnrelatedExplicitType(t *testing.T) {
	store := nestedWindowStore()
	args := []any{map[string]any{
		"customTimeWindows": []any{
			map[string]any{
				"@type":     "com.example.Unrelated",
				"beginDate": "20260701",
			},
		},
	}}
	_, err := NormalizeArgs([]string{"com.example.UpsertReq"}, args, store)
	if err == nil {
		t.Fatal("unrelated explicit @type should be rejected")
	}
	if !strings.Contains(err.Error(), "not assignable") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "com.example.Unrelated") {
		t.Fatalf("error should echo the caller's type name: %v", err)
	}
}
