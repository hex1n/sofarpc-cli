package cli

import (
	"reflect"
	"testing"
)

func TestCanonicalBundledSkillName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		aliased bool
	}{
		{"", callRPCSkillName, false},
		{callRPCSkillName, callRPCSkillName, false},
		{callFacadeSkillAlias, callRPCSkillName, true},
		{invokeFacadeSkillAlias, callRPCSkillName, true},
		{rpcTestSkillAlias, callRPCSkillName, true},
		{"custom-skill", "custom-skill", false},
	}
	for _, tc := range tests {
		got, aliased := canonicalBundledSkillName(tc.in)
		if got != tc.want || aliased != tc.aliased {
			t.Fatalf("canonicalBundledSkillName(%q) = (%q, %v), want (%q, %v)", tc.in, got, aliased, tc.want, tc.aliased)
		}
	}
}

func TestBundledSkillNameCandidates(t *testing.T) {
	if got := bundledSkillNameCandidates(rpcTestSkillAlias); !reflect.DeepEqual(got, []string{callRPCSkillName, callFacadeSkillAlias, invokeFacadeSkillAlias, rpcTestSkillAlias}) {
		t.Fatalf("unexpected alias candidates: %v", got)
	}
	if got := bundledSkillNameCandidates("custom-skill"); !reflect.DeepEqual(got, []string{"custom-skill"}) {
		t.Fatalf("unexpected custom candidates: %v", got)
	}
}

func TestShouldListBundledSkillDir(t *testing.T) {
	if shouldListBundledSkillDir(rpcTestSkillAlias) {
		t.Fatal("legacy alias dir should be hidden from `skills list`")
	}
	if shouldListBundledSkillDir(callFacadeSkillAlias) {
		t.Fatal("deprecated alias dir should be hidden from `skills list`")
	}
	if shouldListBundledSkillDir(invokeFacadeSkillAlias) {
		t.Fatal("deprecated alias dir should be hidden from `skills list`")
	}
	if !shouldListBundledSkillDir(callRPCSkillName) {
		t.Fatal("canonical skill dir should be listed")
	}
}
