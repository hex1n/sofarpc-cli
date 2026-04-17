package metadata

import "testing"

func TestMetadataMatchesRequiresExecutableDigestAndTTL(t *testing.T) {
	meta := daemonMetadata{
		ProtocolVersion:  protocolVersion,
		Executable:       "C:/tools/sofarpc.exe",
		ExecutableDigest: "digest-a",
		CacheTTL:         "30m0s",
	}

	if !metadataMatches(meta, "C:/tools/sofarpc.exe", "digest-a", "30m0s") {
		t.Fatal("expected identical metadata to match")
	}
	if metadataMatches(meta, "C:/tools/sofarpc.exe", "digest-b", "30m0s") {
		t.Fatal("expected digest drift to break match")
	}
	if metadataMatches(meta, "C:/tools/sofarpc.exe", "digest-a", "45m0s") {
		t.Fatal("expected TTL drift to break match")
	}
}
