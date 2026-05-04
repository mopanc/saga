package saga

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempHome sets up an isolated SAGA_HOME for a test and returns its path.
func withTempHome(t *testing.T) (*Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{
		HomeDir: dir,
		DBPath:  filepath.Join(dir, "index.db"),
	}
	return cfg, dir
}

func TestResolver_personalAutoInit(t *testing.T) {
	cfg, home := withTempHome(t)
	r := NewResolver(cfg)

	layers, err := r.Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer (personal only), got %d", len(layers))
	}
	if layers[0].Scope != "personal" {
		t.Errorf("scope = %q, want personal", layers[0].Scope)
	}

	// meta.yml created
	if _, err := os.Stat(filepath.Join(home, "personal", "meta.yml")); err != nil {
		t.Errorf("personal meta.yml not created: %v", err)
	}
	// topics/ created
	if _, err := os.Stat(filepath.Join(home, "personal", "topics")); err != nil {
		t.Errorf("personal topics/ not created: %v", err)
	}
}

func TestResolver_personalIdempotent(t *testing.T) {
	cfg, _ := withTempHome(t)
	r := NewResolver(cfg)

	if _, err := r.Resolve(t.TempDir()); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if _, err := r.Resolve(t.TempDir()); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
}

func TestResolver_discoverProjectByWalkUp(t *testing.T) {
	cfg, _ := withTempHome(t)
	r := NewResolver(cfg)

	// Create a fake project: <tmp>/repo/.saga/meta.yml
	repo := t.TempDir()
	sagaDir := filepath.Join(repo, ".saga")
	if err := os.MkdirAll(filepath.Join(sagaDir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`scope: project:demo
display_name: Demo project
write_policy: direct
notes_dir: topics/
`)
	if err := os.WriteFile(filepath.Join(sagaDir, "meta.yml"), meta, 0o644); err != nil {
		t.Fatal(err)
	}

	// Resolve from a deep subdir
	deep := filepath.Join(repo, "src", "services", "mjpeg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	layers, err := r.Resolve(deep)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}
	if layers[1].Scope != "project:demo" {
		t.Errorf("project scope = %q", layers[1].Scope)
	}
	if layers[1].NotesDir != filepath.Join(sagaDir, "topics/") {
		t.Errorf("notes dir = %q", layers[1].NotesDir)
	}
}

func TestResolver_noProjectFound(t *testing.T) {
	cfg, _ := withTempHome(t)
	r := NewResolver(cfg)

	// Resolve from a temp dir that has no .saga/ anywhere up the chain
	layers, err := r.Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Note: walk-up will eventually hit / which won't have .saga/, so personal-only.
	// (If the test machine has /.saga/ this would fail — practically impossible.)
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer (personal), got %d", len(layers))
	}
}

func TestLoadLayer_invalidMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meta.yml"), []byte("not yaml: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLayer(dir); err == nil {
		t.Fatal("expected error for invalid yaml")
	}
}

func TestLoadLayer_missingScope(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meta.yml"), []byte("display_name: oops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLayer(dir); err == nil {
		t.Fatal("expected error for missing scope")
	}
}
