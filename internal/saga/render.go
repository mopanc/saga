package saga

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

// Render produces the on-disk markdown representation of a Topic:
// YAML frontmatter (between --- delimiters) followed by the body.
// Inverse of ParseTopic — round-trips for any well-formed topic.
func (t *Topic) Render() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		return nil, fmt.Errorf("yaml encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("---\n\n")
	body := strings.TrimRight(t.Body, "\n")
	buf.WriteString(body)
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// Slugify converts a free-form name into a filesystem-safe slug.
// Decomposes accented characters (NFD) and strips combining marks before
// downcasing, so "Memória" → "memoria" rather than "mria".
// Multiple non-alphanumeric runs collapse to a single hyphen.
func Slugify(name string) string {
	t := transform.Chain(norm.NFD, transform.RemoveFunc(func(r rune) bool {
		return unicode.Is(unicode.Mn, r)
	}))
	decomposed, _, _ := transform.String(t, name)
	decomposed = strings.ToLower(decomposed)

	var b strings.Builder
	pendingDash := false
	for _, r := range decomposed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			pendingDash = false
		default:
			pendingDash = true
		}
	}
	out := b.String()
	if out == "" {
		out = "untitled"
	}
	return out
}

// writeFileAtomic writes data to path via a temp file in the same directory,
// then renames into place. Avoids torn writes if the process is killed mid-write
// or the disk fills up between open and final byte.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".saga-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
