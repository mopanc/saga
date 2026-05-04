package saga

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Topic — parsed view of a markdown file with YAML frontmatter.
// The body is the markdown content after the closing `---`.
type Topic struct {
	ID          string           `yaml:"id"`
	Scope       string           `yaml:"scope"`
	Type        string           `yaml:"type"`
	Title       string           `yaml:"title"`
	Synonyms    []string         `yaml:"synonyms,omitempty"`
	Sensitivity string           `yaml:"sensitivity"`
	Confidence  string           `yaml:"confidence"`
	CreatedAt   time.Time        `yaml:"created_at"`
	UpdatedAt   time.Time        `yaml:"updated_at"`
	CreatedBy   string           `yaml:"created_by,omitempty"`
	UpdatedBy   string           `yaml:"updated_by,omitempty"`
	References  []TopicReference `yaml:"references,omitempty"`
	Related     []string         `yaml:"related,omitempty"`
	Body        string           `yaml:"-"`
}

type TopicReference struct {
	Path      string `yaml:"path" json:"path"`
	Lines     string `yaml:"lines,omitempty" json:"lines,omitempty"`
	BlameHash string `yaml:"blame_hash" json:"blame_hash"`
}

var frontmatterDelim = []byte("---\n")

// ParseTopic parses a markdown document of the form:
//
//	---
//	<yaml frontmatter>
//	---
//
//	<markdown body>
//
// Required frontmatter fields: id, scope, type, title.
func ParseTopic(content []byte) (*Topic, error) {
	if !bytes.HasPrefix(content, frontmatterDelim) {
		return nil, fmt.Errorf("missing frontmatter: file must start with ---")
	}
	rest := content[len(frontmatterDelim):]
	end := bytes.Index(rest, frontmatterDelim)
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter: missing closing ---")
	}

	var t Topic
	if err := yaml.Unmarshal(rest[:end], &t); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	body := string(rest[end+len(frontmatterDelim):])
	body = strings.TrimLeft(body, "\n")
	body = strings.TrimRight(body, "\n")
	t.Body = body

	if t.ID == "" {
		return nil, fmt.Errorf("frontmatter missing required field: id")
	}
	if t.Scope == "" {
		return nil, fmt.Errorf("frontmatter missing required field: scope")
	}
	if t.Type == "" {
		return nil, fmt.Errorf("frontmatter missing required field: type")
	}
	if t.Title == "" {
		return nil, fmt.Errorf("frontmatter missing required field: title")
	}

	switch t.Type {
	case "profile", "preference", "policy", "topic":
	default:
		return nil, fmt.Errorf("invalid type %q (want profile|preference|policy|topic)", t.Type)
	}

	return &t, nil
}
