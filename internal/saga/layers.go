package saga

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Meta is the parsed contents of a layer's meta.yml.
type Meta struct {
	Scope              string   `yaml:"scope"`
	DisplayName        string   `yaml:"display_name,omitempty"`
	Inherits           []string `yaml:"inherits,omitempty"`
	SensitivityDefault string   `yaml:"sensitivity_default,omitempty"`
	WritePolicy        string   `yaml:"write_policy,omitempty"`
	NotesDir           string   `yaml:"notes_dir,omitempty"`
}

// Layer is an active scope (personal, project, dept, org) with its on-disk root.
type Layer struct {
	Scope    string
	RootPath string // dir containing meta.yml
	NotesDir string // resolved absolute path to notes directory
	Meta     Meta
}

// Resolver discovers layers given a cwd and the user's saga home.
type Resolver struct {
	homeDir string
}

func NewResolver(cfg *Config) *Resolver {
	return &Resolver{homeDir: cfg.HomeDir}
}

// Resolve returns the layers active for a given cwd, in retrieval-merge order:
// personal first, then project (if discovered).
//
// The personal layer is auto-initialised on first call (creates skeleton
// meta.yml and topics/ dir under ~/.saga/personal/).
//
// Phase 1 ignores `inherits:` from project meta — dept/org layers land in
// Phase 2. The discovered project's inherits are recorded on Layer.Meta but
// not loaded.
func (r *Resolver) Resolve(cwd string) ([]Layer, error) {
	personal, err := r.loadOrInitPersonal()
	if err != nil {
		return nil, fmt.Errorf("personal layer: %w", err)
	}
	layers := []Layer{personal}

	project, err := r.discoverProject(cwd)
	if err != nil {
		return nil, fmt.Errorf("discover project: %w", err)
	}
	if project != nil {
		layers = append(layers, *project)
	}
	return layers, nil
}

func (r *Resolver) loadOrInitPersonal() (Layer, error) {
	root := filepath.Join(r.homeDir, "personal")
	metaPath := filepath.Join(root, "meta.yml")

	if _, err := os.Stat(metaPath); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Join(root, "topics"), 0o700); err != nil {
			return Layer{}, fmt.Errorf("mkdir personal topics: %w", err)
		}
		seed := []byte(`scope: personal
display_name: Personal layer
sensitivity_default: internal
write_policy: direct
notes_dir: topics/
`)
		if err := os.WriteFile(metaPath, seed, 0o600); err != nil {
			return Layer{}, fmt.Errorf("write personal meta: %w", err)
		}
	} else if err != nil {
		return Layer{}, err
	}
	return loadLayer(root)
}

func (r *Resolver) discoverProject(cwd string) (*Layer, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	dir := abs
	for {
		metaPath := filepath.Join(dir, ".saga", "meta.yml")
		if _, err := os.Stat(metaPath); err == nil {
			layer, err := loadLayer(filepath.Join(dir, ".saga"))
			if err != nil {
				return nil, err
			}
			return &layer, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

func loadLayer(root string) (Layer, error) {
	metaBytes, err := os.ReadFile(filepath.Join(root, "meta.yml")) // #nosec G304 -- root is a layer dir resolved by Resolver from internal config
	if err != nil {
		return Layer{}, fmt.Errorf("read meta: %w", err)
	}
	var meta Meta
	if err := yaml.Unmarshal(metaBytes, &meta); err != nil {
		return Layer{}, fmt.Errorf("parse meta: %w", err)
	}
	if meta.Scope == "" {
		return Layer{}, fmt.Errorf("meta.yml at %s missing required field: scope", root)
	}
	notesDir := meta.NotesDir
	if notesDir == "" {
		notesDir = "topics/"
	}
	return Layer{
		Scope:    meta.Scope,
		RootPath: root,
		NotesDir: filepath.Join(root, notesDir),
		Meta:     meta,
	}, nil
}
