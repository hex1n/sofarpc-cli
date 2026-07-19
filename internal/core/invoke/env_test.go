package invoke

import (
	"reflect"
	"testing"
)

// TestEnvFlag_MatchesStrconvParseBool locks the shared guardrail parser to
// strict strconv.ParseBool semantics: every entrypoint (MCP tools, call CLI)
// reads opt-ins through EnvFlag, so this table is the single source of truth
// for which env spellings enable a real invoke.
func TestEnvFlag_MatchesStrconvParseBool(t *testing.T) {
	const name = "SOFARPC_TEST_ENV_FLAG"
	cases := []struct {
		raw  string
		want bool
	}{
		{"1", true},
		{"t", true},
		{"T", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"  true  ", true},
		// Mixed-case spellings outside ParseBool's exact set must stay off.
		{"TRUe", false},
		{"tRue", false},
		{"yes", false},
		{"on", false},
		{"0", false},
		{"false", false},
		{"", false},
		{"2", false},
	}
	for _, tc := range cases {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(name, tc.raw)
			if got := EnvFlag(name); got != tc.want {
				t.Fatalf("EnvFlag(%q): got %v want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestEnvList_SplitsAndTrims(t *testing.T) {
	const name = "SOFARPC_TEST_ENV_LIST"

	t.Setenv(name, " 127.0.0.1 , ,dev-rpc.example.com,, 10.0.0.1")
	got := EnvList(name)
	want := []string{"127.0.0.1", "dev-rpc.example.com", "10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EnvList: got %v want %v", got, want)
	}

	t.Setenv(name, "   ")
	if got := EnvList(name); got != nil {
		t.Fatalf("blank env should yield nil, got %v", got)
	}
}
