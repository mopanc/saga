package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jorgemorais/saga/internal/saga"
)

const projectMetaTemplate = `scope: project:%s
display_name: %s
write_policy: direct
notes_dir: topics/
`

const topicsKeep = `# Topic notes for this project live here.
# Each .md file is one topic — see https://github.com/jorgemorais/saga
# for the format.
`

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	name := fs.String("name", "", "Project scope name (default: detected from git or cwd basename)")
	displayName := fs.String("display-name", "", "Human display name (default: derived from scope)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "saga init — create .saga/ in the current directory with a project meta.yml.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	sagaDir := filepath.Join(cwd, ".saga")
	if _, err := os.Stat(sagaDir); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", sagaDir)
	}

	scope := *name
	if scope == "" {
		scope = detectProjectName(cwd)
	}
	display := *displayName
	if display == "" {
		display = strings.ReplaceAll(scope, "-", " ")
		display = strings.Title(display) //nolint:staticcheck — fine for ASCII project names
	}

	if err := os.MkdirAll(filepath.Join(sagaDir, "topics"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(sagaDir, "topics", "README.md"), []byte(topicsKeep), 0o644); err != nil {
		return err
	}

	metaPath := filepath.Join(sagaDir, "meta.yml")
	metaContent := fmt.Sprintf(projectMetaTemplate, scope, display)
	if err := os.WriteFile(metaPath, []byte(metaContent), 0o644); err != nil {
		return err
	}

	fmt.Printf("Initialised .saga/ in %s\n", cwd)
	fmt.Printf("  scope:   project:%s\n", scope)
	fmt.Printf("  meta:    %s\n", metaPath)
	fmt.Printf("  topics:  %s\n", filepath.Join(sagaDir, "topics"))
	fmt.Println()
	fmt.Println("Commit .saga/ to your project repo so the layer travels with the code.")
	fmt.Println("(See `saga setup-claude` to wire saga into Claude Code.)")
	return nil
}

// detectProjectName tries (in order): git repo basename, cwd basename.
func detectProjectName(cwd string) string {
	if root, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
		return saga.Slugify(filepath.Base(strings.TrimSpace(string(root))))
	}
	return saga.Slugify(filepath.Base(cwd))
}
