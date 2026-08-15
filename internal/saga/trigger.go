package saga

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Enforcement levels a topic can declare.
const (
	EnforcementAdvise = "advise" // inject the rule alongside the action (default)
	EnforcementBlock  = "block"  // refuse the action and explain why
)

// TriggeredRule is a topic whose triggers matched an action the agent is about
// to take.
type TriggeredRule struct {
	ID          string
	Title       string
	Scope       string
	Enforcement string
	FilePath    string
	Body        string
}

// MatchTrigger reports whether a trigger pattern matches an action identifier.
//
// Two forms, both host-neutral — the engine matches strings and never
// interprets what an action means:
//
//	"Bash"              bare name: matches any action with that name
//	"Bash(git commit*)"  name plus an argument glob, `*` matching any run of
//	                     characters (including none)
//
// Name comparison is exact and case-sensitive; hosts own their namespace and
// are expected to be consistent within it. The argument glob is matched against
// whatever the host presents as the action's argument string.
func MatchTrigger(pattern, actionName, actionArg string) bool {
	name, arg, hasArg := strings.Cut(pattern, "(")
	name = strings.TrimSpace(name)
	if name != actionName {
		return false
	}
	if !hasArg {
		return true
	}
	arg = strings.TrimSuffix(strings.TrimSpace(arg), ")")
	return matchGlob(arg, actionArg)
}

// matchGlob matches a pattern containing `*` wildcards against s. Anchored at
// both ends: "git commit*" matches "git commit -m x" but not "sudo git commit".
// Linear scan rather than regex — patterns come from user notes, and compiling
// note content into regexes invites both surprise and pathological backtracking.
func matchGlob(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, part := range parts[1 : len(parts)-1] {
		idx := strings.Index(s, part)
		if idx < 0 {
			return false
		}
		s = s[idx+len(part):]
	}
	return strings.HasSuffix(s, last)
}

// RulesForAction returns the topics in the active layers whose triggers match
// the given action, most restrictive first (blocking rules ahead of advisory
// ones) so a caller that acts on the first match cannot miss a hard rule.
//
// Only topics with a non-empty `triggers` list are considered: a rule with no
// declared triggers has not claimed any action, and guessing on its behalf
// would make injection unpredictable.
func (s *Service) RulesForAction(actionName, actionArg string) ([]TriggeredRule, error) {
	layers, err := s.resolver.Resolve(s.cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve layers: %w", err)
	}
	if len(layers) == 0 {
		return nil, nil
	}

	scopes := make([]any, 0, len(layers))
	for _, l := range layers {
		scopes = append(scopes, l.Scope)
	}
	placeholders := strings.Repeat("?,", len(scopes))
	placeholders = placeholders[:len(placeholders)-1]

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, title, scope, triggers, enforcement, file_path
		FROM topic_index
		WHERE scope IN (%s) AND triggers != '[]' AND triggers != ''
		ORDER BY scope, title
	`, placeholders), scopes...)
	if err != nil {
		return nil, fmt.Errorf("query triggered topics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var advise, block []TriggeredRule
	for rows.Next() {
		var r TriggeredRule
		var trigJSON string
		if err := rows.Scan(&r.ID, &r.Title, &r.Scope, &trigJSON, &r.Enforcement, &r.FilePath); err != nil {
			return nil, err
		}
		var triggers []string
		if err := json.Unmarshal([]byte(trigJSON), &triggers); err != nil {
			continue // malformed triggers must not break the action path
		}
		matched := false
		for _, p := range triggers {
			if MatchTrigger(p, actionName, actionArg) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if r.Enforcement == EnforcementBlock {
			block = append(block, r)
		} else {
			advise = append(advise, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return append(block, advise...), nil
}
