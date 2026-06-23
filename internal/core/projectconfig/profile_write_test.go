package projectconfig

import (
	"strings"
	"testing"
)

func TestSetProfile_AddsWithoutMutatingInput(t *testing.T) {
	original := Config{Profiles: map[string]ProfileConfig{
		"a": {DirectURL: "bolt://a:12200"},
	}}
	updated := SetProfile(original, "b", ProfileConfig{DirectURL: "bolt://b:12200"})

	if len(updated.Profiles) != 2 || updated.Profiles["a"].DirectURL != "bolt://a:12200" || updated.Profiles["b"].DirectURL != "bolt://b:12200" {
		t.Fatalf("updated profiles: %+v", updated.Profiles)
	}
	// The original map must be untouched (SetProfile copies).
	if len(original.Profiles) != 1 {
		t.Fatalf("SetProfile mutated the input map: %+v", original.Profiles)
	}
}

func TestHasProfileAndProfileHasFields(t *testing.T) {
	cfg := Config{Profiles: map[string]ProfileConfig{"test ": {}}}
	if !HasProfile(Normalize(cfg), "test") {
		t.Fatalf("HasProfile should find a trimmed profile name")
	}
	if HasProfile(cfg, "nope") {
		t.Fatalf("HasProfile should not find an undefined profile")
	}
	if ProfileHasFields(ProfileConfig{}) {
		t.Fatalf("an empty profile has no fields")
	}
	if !ProfileHasFields(ProfileConfig{Protocol: "bolt"}) {
		t.Fatalf("a profile with a wire field has fields")
	}
}

func TestMarshalMerged_PreservesExplicitEmptyAllowlist(t *testing.T) {
	cfg := Config{
		DirectURL:       "bolt://base:12200",
		AllowedServices: nil, // explicit-empty, signalled by the set flag below
	}

	// allowedServicesSet=true with an empty list must keep the block-all marker.
	body, err := MarshalMerged(cfg, true)
	if err != nil {
		t.Fatalf("MarshalMerged set: %v", err)
	}
	if !strings.Contains(string(body), `"allowedServices": []`) {
		t.Fatalf("explicit-empty allowlist should round trip as []:\n%s", body)
	}

	// Without the set flag, an empty allowlist is omitted (matches Marshal).
	body, err = MarshalMerged(cfg, false)
	if err != nil {
		t.Fatalf("MarshalMerged unset: %v", err)
	}
	if strings.Contains(string(body), "allowedServices") {
		t.Fatalf("unset empty allowlist should be omitted:\n%s", body)
	}
}

func TestMarshalMerged_MatchesMarshalWhenAllowlistPresent(t *testing.T) {
	cfg := Config{DirectURL: "bolt://base:12200", AllowedServices: []string{"com.foo.Svc"}}
	merged, err := MarshalMerged(cfg, true)
	if err != nil {
		t.Fatalf("MarshalMerged: %v", err)
	}
	plain, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(merged) != string(plain) {
		t.Fatalf("MarshalMerged should match Marshal when allowlist is non-empty:\nmerged=%s\nplain=%s", merged, plain)
	}
}
