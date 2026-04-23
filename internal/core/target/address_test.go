package target

import "testing"

func TestParseDirectDialAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare host", raw: "127.0.0.1", want: "127.0.0.1:12200"},
		{name: "host port", raw: "127.0.0.1:12201", want: "127.0.0.1:12201"},
		{name: "bolt host", raw: "bolt://127.0.0.1", want: "127.0.0.1:12200"},
		{name: "bolt host port", raw: "bolt://127.0.0.1:12201", want: "127.0.0.1:12201"},
		{name: "bracketed ipv6", raw: "[::1]", want: "[::1]:12200"},
		{name: "bracketed ipv6 port", raw: "[::1]:12201", want: "[::1]:12201"},
		{name: "trim spaces", raw: "  bolt://localhost  ", want: "localhost:12200"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDirectDialAddress(tt.raw)
			if err != nil {
				t.Fatalf("ParseDirectDialAddress(%q) returned error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseDirectDialAddress(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseDirectDialAddressErrors(t *testing.T) {
	t.Parallel()

	tests := []string{"", "   ", "bolt://"}
	for _, raw := range tests {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if got, err := ParseDirectDialAddress(raw); err == nil {
				t.Fatalf("ParseDirectDialAddress(%q) = %q, want error", raw, got)
			}
		})
	}
}

func TestParseRegistryDialAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare host", raw: "zk.example.com", want: "zk.example.com:2181"},
		{name: "host port", raw: "zk.example.com:2182", want: "zk.example.com:2182"},
		{name: "scheme host", raw: "zookeeper://zk.example.com", want: "zk.example.com:2181"},
		{name: "scheme host port", raw: "zookeeper://zk.example.com:2182", want: "zk.example.com:2182"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRegistryDialAddress(tt.raw)
			if err != nil {
				t.Fatalf("ParseRegistryDialAddress(%q) returned error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRegistryDialAddress(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
