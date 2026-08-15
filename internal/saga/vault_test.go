package saga

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildVault_linksEveryLayer(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	personal := setupProjectLayer(t, "personal", map[string]string{
		"a.md": noteInScope("01HXY5KZQVJ8M3R7ABCDEFGH01", "personal", "Personal note"),
	})
	project := setupProjectLayer(t, "project:demo", map[string]string{
		"b.md": noteInScope("01HXY5KZQVJ8M3R7ABCDEFGH02", "project:demo", "Project note"),
	})
	for _, l := range []Layer{personal, project} {
		if _, err := db.IndexLayer(l); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(db, &Config{HomeDir: t.TempDir()}, t.TempDir())
	root := filepath.Join(t.TempDir(), "vault")

	res, err := svc.BuildVault(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Links) != 2 {
		t.Fatalf("linked %d layers, want 2: %+v", len(res.Links), res.Links)
	}

	// The scope prefix carries no information once each layer is its own folder.
	if _, err := os.Lstat(filepath.Join(root, "demo")); err != nil {
		t.Errorf("project:demo should link as \"demo\": %v", err)
	}
	// Notes must be reachable through the link, not copied.
	if _, err := os.Stat(filepath.Join(root, "personal", "a.md")); err != nil {
		t.Errorf("note not reachable through the vault link: %v", err)
	}

	// Idempotent: a second run changes nothing.
	again, err := svc.BuildVault(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range again.Links {
		if l.Created {
			t.Errorf("second run recreated the link for %s", l.Scope)
		}
	}
}

// TestBuildVault_refusesToClobberRealDirectory is the safety property: a vault
// path holding real data is either a mistake or someone's files, and removing
// it to make room is not this command's call.
func TestBuildVault_refusesToClobberRealDirectory(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "personal", map[string]string{
		"a.md": noteInScope("01HXY5KZQVJ8M3R7ABCDEFGH01", "personal", "Note"),
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "vault")
	occupied := filepath.Join(root, "personal")
	if err := os.MkdirAll(occupied, 0o700); err != nil {
		t.Fatal(err)
	}
	precious := filepath.Join(occupied, "do-not-delete.md")
	if err := os.WriteFile(precious, []byte("real data"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(db, &Config{HomeDir: t.TempDir()}, t.TempDir())
	if _, err := svc.BuildVault(root); err == nil {
		t.Fatal("expected an error rather than clobbering a real directory")
	}
	if _, err := os.Stat(precious); err != nil {
		t.Errorf("real file was destroyed building the vault: %v", err)
	}
}
