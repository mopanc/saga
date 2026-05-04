package saga

import (
	"strings"
	"unicode"
)

// sanitizeFTSQuery converts arbitrary user input into a safe FTS5 MATCH
// expression, aligned with the unicode61 tokenizer (which splits on every
// non-alphanumeric rune). Each surviving token is wrapped in double quotes
// and joined by OR for broad recall.
//
// FTS5 reserved keywords (AND, OR, NOT, NEAR) are dropped to avoid syntax
// confusion when the user types them as terms. Returns "" when no usable
// tokens remain — callers must short-circuit and return zero results.
//
// This corrects the v1 bug where queries were tokenised differently from
// stored text (hyphen-stripped vs hyphen-split), causing valid matches to
// silently miss.
func sanitizeFTSQuery(input string) string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		tok := string(cur)
		cur = cur[:0]
		switch strings.ToUpper(tok) {
		case "AND", "OR", "NOT", "NEAR":
			return
		}
		tokens = append(tokens, `"`+tok+`"`)
	}
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return strings.Join(tokens, " OR ")
}
