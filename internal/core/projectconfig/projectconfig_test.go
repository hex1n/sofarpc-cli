package projectconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead_LoadsAndNormalizesProjectConfig(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, KindLocal, `{
  "directUrl": " bolt://project-host:12200 ",
  "protocol": " bolt ",
  "allowedServices": [" com.foo.UserFacade ", " "]
}`)

	result, err := Read(root, KindLocal)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !result.Exists || result.Kind != KindLocal || result.Path == "" {
		t.Fatalf("result metadata: %+v", result)
	}
	if result.Config.DirectURL != "bolt://project-host:12200" {
		t.Fatalf("directUrl: %q", result.Config.DirectURL)
	}
	if result.Config.Protocol != "bolt" {
		t.Fatalf("protocol: %q", result.Config.Protocol)
	}
	if !result.AllowedServicesSet {
		t.Fatalf("allowedServices should be marked configured: %+v", result)
	}
	if len(result.Config.AllowedServices) != 1 || result.Config.AllowedServices[0] != "com.foo.UserFacade" {
		t.Fatalf("allowedServices: %#v", result.Config.AllowedServices)
	}
}

func TestRead_ExplicitEmptyAllowedServicesIsConfigured(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, KindShared, `{"allowedServices":[]}`)

	result, err := Read(root, KindShared)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !result.Exists || !result.AllowedServicesSet {
		t.Fatalf("empty allowedServices should count as configured: %+v", result)
	}
	if len(result.Config.AllowedServices) != 0 {
		t.Fatalf("allowedServices: %#v", result.Config.AllowedServices)
	}
}

func TestMarshal_NormalizesProjectConfig(t *testing.T) {
	body, err := Marshal(Config{
		DirectURL:       " bolt://project-host:12200 ",
		Protocol:        " bolt ",
		AllowedServices: []string{" com.foo.UserFacade ", " "},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"directUrl": "bolt://project-host:12200"`) {
		t.Fatalf("directUrl was not normalized:\n%s", text)
	}
	if !strings.Contains(text, `"protocol": "bolt"`) {
		t.Fatalf("protocol was not normalized:\n%s", text)
	}
	if strings.Contains(text, `" "`) || !strings.Contains(text, `"com.foo.UserFacade"`) {
		t.Fatalf("allowedServices were not normalized:\n%s", text)
	}
}

func TestRead_RejectsInvalidProjectConfig(t *testing.T) {
	tests := map[string]string{
		"unknown field":        `{"mode":"direct"}`,
		"multiple json values": `{"directUrl":"bolt://a:1"} {}`,
		"direct and registry":  `{"directUrl":"bolt://a:1","registryAddress":"zookeeper://zk:2181"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeConfigFile(t, root, KindShared, body)

			result, err := Read(root, KindShared)
			if err == nil {
				t.Fatalf("expected read error, got result %+v", result)
			}
			if !result.Exists {
				t.Fatalf("invalid existing file should report Exists: %+v", result)
			}
		})
	}
}

func TestRead_MissingConfigIsNotAnError(t *testing.T) {
	root := t.TempDir()
	result, err := Read(root, KindLocal)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.Exists {
		t.Fatalf("missing config should not exist: %+v", result)
	}
	if !strings.HasSuffix(result.Path, filepath.Join(".sofarpc", "config.local.json")) {
		t.Fatalf("path: %q", result.Path)
	}
}

func writeConfigFile(t *testing.T, root string, kind Kind, body string) {
	t.Helper()
	path := ConfigPath(root, kind)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
