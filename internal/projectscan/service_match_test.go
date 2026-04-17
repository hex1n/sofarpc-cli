package projectscan

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchServicePrefersSourceOverJar(t *testing.T) {
	root := t.TempDir()
	module := FacadeModule{
		Name:            "svc-facade",
		SourceRoot:      "svc-facade/src/main/java",
		MavenModulePath: "svc-facade",
	}
	sourceRoot := filepath.Join(root, "svc-facade", "src", "main", "java", "com", "example")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceRoot: %v", err)
	}
	sourceFile := filepath.Join(sourceRoot, "UserFacade.java")
	if err := os.WriteFile(sourceFile, []byte("interface UserFacade {}"), 0o644); err != nil {
		t.Fatalf("WriteFile sourceFile: %v", err)
	}
	jarPath := filepath.Join(root, "svc-facade", "target", "svc-facade-1.0.0.jar")
	if err := writeJarWithEntries(jarPath, []string{"com/example/UserFacade.class"}); err != nil {
		t.Fatalf("writeJarWithEntries(): %v", err)
	}

	match, err := MatchService(root, "com.example.UserFacade", []FacadeModule{module})
	if err != nil {
		t.Fatalf("MatchService() error = %v", err)
	}
	if match.MatchKind != "source" || match.MatchPath != sourceFile {
		t.Fatalf("match = %+v", match)
	}
}

func TestMatchServiceFallsBackToPrimaryJar(t *testing.T) {
	root := t.TempDir()
	module := FacadeModule{
		Name:            "svc-facade",
		MavenModulePath: "svc-facade",
	}
	jarPath := filepath.Join(root, "svc-facade", "target", "svc-facade-1.0.0.jar")
	if err := writeJarWithEntries(jarPath, []string{"com/example/UserFacade.class"}); err != nil {
		t.Fatalf("writeJarWithEntries(): %v", err)
	}

	match, err := MatchService(root, "com.example.UserFacade", []FacadeModule{module})
	if err != nil {
		t.Fatalf("MatchService() error = %v", err)
	}
	if match.MatchKind != "primary-jar" || match.MatchPath != jarPath {
		t.Fatalf("match = %+v", match)
	}
}

func TestMatchServiceReturnsNotFoundForUnknownService(t *testing.T) {
	root := t.TempDir()
	module := FacadeModule{
		Name:            "svc-facade",
		MavenModulePath: "svc-facade",
	}
	jarPath := filepath.Join(root, "svc-facade", "target", "svc-facade-1.0.0.jar")
	if err := writeJarWithEntries(jarPath, []string{"com/example/UserFacade.class"}); err != nil {
		t.Fatalf("writeJarWithEntries(): %v", err)
	}

	if _, err := MatchService(root, "com.example.OrderFacade", []FacadeModule{module}); err == nil {
		t.Fatal("MatchService() error = nil, want error")
	}
}

func TestMatchServiceReturnsAmbiguousWhenMultipleModulesContainSameService(t *testing.T) {
	root := t.TempDir()
	modules := []FacadeModule{
		{Name: "svc-a-facade", MavenModulePath: "svc-a-facade"},
		{Name: "svc-b-facade", MavenModulePath: "svc-b-facade"},
	}
	for _, module := range modules {
		jarPath := filepath.Join(root, module.MavenModulePath, "target", module.Name+"-1.0.0.jar")
		if err := writeJarWithEntries(jarPath, []string{"com/example/UserFacade.class"}); err != nil {
			t.Fatalf("writeJarWithEntries(%s): %v", module.Name, err)
		}
	}

	if _, err := MatchService(root, "com.example.UserFacade", modules); err == nil {
		t.Fatal("MatchService() error = nil, want ambiguous error")
	}
}

func writeJarWithEntries(path string, entries []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for _, entry := range entries {
		w, err := writer.Create(entry)
		if err != nil {
			writer.Close()
			return err
		}
		if _, err := w.Write([]byte("stub")); err != nil {
			writer.Close()
			return err
		}
	}
	return writer.Close()
}
