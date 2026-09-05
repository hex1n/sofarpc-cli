package target

import "testing"

func TestFormatConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		in   []ConfigError
		want string
	}{
		{"none", nil, ""},
		{"path and message", []ConfigError{{Path: "a.json", Error: "bad"}}, "a.json: bad"},
		{"path only", []ConfigError{{Path: "a.json"}}, "a.json"},
		{"message only", []ConfigError{{Error: "bad"}}, "bad"},
		{"blank entry is dropped", []ConfigError{{Path: "  ", Error: " "}}, ""},
		{
			"several are joined",
			[]ConfigError{{Path: "a.json", Error: "bad"}, {Path: "b.json", Error: "worse"}},
			"a.json: bad; b.json: worse",
		},
		{
			"a blank entry does not leave a stray separator",
			[]ConfigError{{Path: "a.json", Error: "bad"}, {}, {Error: "worse"}},
			"a.json: bad; worse",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatConfigErrors(tc.in); got != tc.want {
				t.Fatalf("FormatConfigErrors(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
