package target

import (
	"strings"
	"testing"
)

func TestParseDirectDialAddressLegacyCases(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{"empty", "", "", "directUrl is empty"},
		{"whitespace only", "   ", "", "directUrl is empty"},
		{"scheme host port", "bolt://host:12201", "host:12201", ""},
		{"scheme host default port", "bolt://host", "host:12200", ""},
		{"plain host port", "host:12345", "host:12345", ""},
		{"plain host default port", "plainhost", "plainhost:12200", ""},
		{"scheme missing host", "bolt://", "", "no host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDirectDialAddress(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err: got %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseRegistryDialAddressLegacyCases(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{"empty", "", "", "registryAddress is empty"},
		{"scheme host port", "zookeeper://zk:2182", "zk:2182", ""},
		{"scheme host default port", "zookeeper://zk", "zk:2181", ""},
		{"plain host port", "zk:2182", "zk:2182", ""},
		{"plain host default port", "zk", "zk:2181", ""},
		{"scheme missing host", "zookeeper://", "", "no host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRegistryDialAddress(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err: got %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// IPv6 literals are the spicy case — net.SplitHostPort disagrees with url.Parse
// about bracket handling, and we want to make sure missing-port + present-port
// forms both round-trip cleanly.
func TestEnsurePort(t *testing.T) {
	cases := []struct {
		name, in, defaultPort, want string
	}{
		{"empty returns empty", "", "12200", ""},
		{"v4 no port gets default", "host", "12200", "host:12200"},
		{"v4 with port unchanged", "host:9999", "12200", "host:9999"},
		{"v6 bracketed no port gets default", "[::1]", "12200", "[::1]:12200"},
		{"v6 bracketed with port unchanged", "[::1]:12201", "12200", "[::1]:12201"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ensurePort(tc.in, tc.defaultPort); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// Probe with an unparseable direct URL should surface the parse error as the
// probe message (not crash, not connect).
func TestProbe_DirectWithEmptyURLReportsParseError(t *testing.T) {
	got := Probe(Config{Mode: ModeDirect, DirectURL: ""})
	if got.Reachable {
		t.Fatal("empty direct URL should not be reachable")
	}
	if !strings.Contains(got.Message, "directUrl is empty") {
		t.Fatalf("expected parse error in message, got %q", got.Message)
	}
}

func TestProbe_RegistryWithEmptyAddressReportsParseError(t *testing.T) {
	got := Probe(Config{Mode: ModeRegistry, RegistryAddress: ""})
	if got.Reachable {
		t.Fatal("empty registry address should not be reachable")
	}
	if !strings.Contains(got.Message, "registryAddress is empty") {
		t.Fatalf("expected parse error in message, got %q", got.Message)
	}
}

func TestProbe_UnknownModeReportsError(t *testing.T) {
	got := Probe(Config{Mode: "bogus"})
	if got.Reachable {
		t.Fatal("unknown mode should not be reachable")
	}
	if !strings.Contains(got.Message, "unknown target mode") {
		t.Fatalf("expected unknown mode error, got %q", got.Message)
	}
}
