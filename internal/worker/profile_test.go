package worker

import "testing"

func TestProfileKey_DeterministicAndDistinct(t *testing.T) {
	a := Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "deadbeef", JavaMajor: 17}
	b := Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "deadbeef", JavaMajor: 17}
	if a.Key() != b.Key() {
		t.Fatalf("identical profiles should produce identical keys: %s vs %s", a.Key(), b.Key())
	}
	if len(a.Key()) != 64 {
		t.Fatalf("expected 256-bit hex digest (64 chars), got %d", len(a.Key()))
	}

	c := Profile{SOFARPCVersion: "5.12.1", RuntimeJarDigest: "deadbeef", JavaMajor: 17}
	if a.Key() == c.Key() {
		t.Fatal("differing sofarpc versions must produce different keys")
	}
	d := Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "deadbeef", JavaMajor: 21}
	if a.Key() == d.Key() {
		t.Fatal("differing java majors must produce different keys")
	}
}

func TestProfileKey_DoesNotIncludeStubJars(t *testing.T) {
	// Architecture §7.1 / improvement-plan §5: stubs live on the request,
	// not on the profile. Two profiles with different stub jars (which
	// the Profile struct can't even express) must still key the same.
	a := Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "abc", JavaMajor: 17}
	b := Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "abc", JavaMajor: 17}
	if a.Key() != b.Key() {
		t.Fatal("profile key must depend only on the three documented fields")
	}
}

func TestProfileEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   Profile
		want bool
	}{
		{"complete", Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "x", JavaMajor: 17}, false},
		{"missing version", Profile{RuntimeJarDigest: "x", JavaMajor: 17}, true},
		{"missing digest", Profile{SOFARPCVersion: "5.12.0", JavaMajor: 17}, true},
		{"missing java", Profile{SOFARPCVersion: "5.12.0", RuntimeJarDigest: "x"}, true},
		{"zero", Profile{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Empty(); got != tc.want {
				t.Fatalf("Empty()=%v want %v", got, tc.want)
			}
		})
	}
}

func TestClassloaderKey_OrderInsensitive(t *testing.T) {
	a := ClassloaderKey([]string{"/a.jar", "/b.jar", "/c.jar"})
	b := ClassloaderKey([]string{"/c.jar", "/a.jar", "/b.jar"})
	if a != b {
		t.Fatalf("classloader key should be order-insensitive: %s vs %s", a, b)
	}
}

func TestClassloaderKey_EmptyIsSentinel(t *testing.T) {
	if got := ClassloaderKey(nil); got != "none" {
		t.Fatalf("empty stub list should produce sentinel 'none', got %q", got)
	}
	if got := ClassloaderKey([]string{}); got != "none" {
		t.Fatalf("empty stub slice should produce sentinel 'none', got %q", got)
	}
}

func TestClassloaderKey_DifferentJarsDiffer(t *testing.T) {
	a := ClassloaderKey([]string{"/a.jar"})
	b := ClassloaderKey([]string{"/b.jar"})
	if a == b {
		t.Fatal("different jar sets must produce different classloader keys")
	}
}
