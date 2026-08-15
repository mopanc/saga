package saga

import (
	"fmt"
	"strings"
)

// DefaultRuleCatalogMaxTokens caps the always-on rule catalogue. Sized against
// a real store: 11 policy notes render to ~2.4k chars (~610 tokens) including
// the header, so 700 leaves headroom for a few more rules before any is
// dropped. Deliberately far below what the bodies would cost (~6.3k tokens on
// the personal layer alone) — the catalogue carries pointers, not content.
const DefaultRuleCatalogMaxTokens = 700

// BuildRuleCatalog renders an always-on index of the `policy` notes in force
// for the current working directory, across every active layer. Returns the
// rendered block (empty when there are no policies), the ids that contributed,
// and error.
//
// Why an index and not the rules themselves: injecting every policy body costs
// ~6.3k tokens on the personal layer alone, on every prompt, forever. A
// catalogue of the same 11 rules costs a few hundred. The rules are still
// reachable — the model reads the one it needs via topic_read.
//
// Why this exists at all: policies were previously invisible to the always-on
// path (the identity baseline queries profile+preference only), so a rule was
// injected only when the prompt happened to match it lexically. Rules the user
// had explicitly written were therefore broken in silence, which is the exact
// failure the layered design is supposed to prevent.
//
// Selecting on `confidence: canonical` was considered and rejected: in the
// field every policy note carries the parser default `proposed`, so that
// selector injects nothing. Type is the honest selector — the user typed a note
// as `policy` precisely to say "this is a rule".
//
// The catalogue is a portable fallback, not the enforcement mechanism. It
// depends on the model noticing an entry applies. Clients that support
// tool-call interception should also inject the matching rule body at the
// point of use, where it is deterministic rather than a matter of attention.
func (s *Service) BuildRuleCatalog(maxTokens int) (string, []string, error) {
	if maxTokens <= 0 {
		maxTokens = DefaultRuleCatalogMaxTokens
	}

	layers, err := s.resolver.Resolve(s.cwd)
	if err != nil {
		return "", nil, fmt.Errorf("resolve layers: %w", err)
	}
	// Most specific layer first: rules from the project you are standing in are
	// likelier to bear on the work than broad personal ones, and if the budget
	// forces a drop it should fall on the general end.
	scopes := make([]string, 0, len(layers))
	for i := len(layers) - 1; i >= 0; i-- {
		scopes = append(scopes, layers[i].Scope)
	}

	all, err := s.notesByScopesAndType(scopes, []string{"policy"})
	if err != nil {
		return "", nil, fmt.Errorf("query policy notes: %w", err)
	}

	// A rule that declares `triggers` is delivered by the guard at the moment
	// the action it governs happens, so listing it here as well is redundant
	// spend on every prompt. The catalogue carries the remainder: rules that
	// cannot be tied to an action and therefore have no deterministic delivery.
	//
	// This makes the always-on cost fall as rules gain triggers, rather than
	// grow with the store.
	policies := make([]*Topic, 0, len(all))
	deterministic := 0
	for _, t := range all {
		if len(t.Triggers) > 0 {
			deterministic++
			continue
		}
		policies = append(policies, t)
	}
	if len(policies) == 0 {
		return "", nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Rules in force here (%d)\n\n", len(policies))
	sb.WriteString("These are rules the user has written and expects followed. " +
		"Listed by name and trigger phrases only. Before acting on anything one " +
		"of them covers, read it in full with topic_read — do not act from the " +
		"summary line.\n")
	if deterministic > 0 {
		fmt.Fprintf(&sb, "\n%d further rule(s) are bound to specific actions and will be "+
			"delivered when those actions occur; they are not listed here.\n", deterministic)
	}
	sb.WriteString("\n")

	// Truncate entry-wise, never by character boundary. The catalogue is a list
	// whose entries are individually meaningful: cutting it at the last
	// paragraph break drops the whole list (every entry sits under one header),
	// which is the same first-come-first-served failure the identity baseline
	// had. Dropping whole rules and saying so is honest; silently emitting a
	// header with nothing under it is not.
	budget := maxTokens * 4
	used := sb.Len()
	ids := make([]string, 0, len(policies))
	dropped := 0

	for _, t := range policies {
		var line strings.Builder
		fmt.Fprintf(&line, "- **%s** `[%s]`", t.Title, t.Scope)
		if len(t.Synonyms) > 0 {
			fmt.Fprintf(&line, " — applies to: %s", strings.Join(t.Synonyms, ", "))
		}
		line.WriteString("\n")

		// Always keep room for the overflow notice.
		if used+line.Len() > budget-96 && len(ids) > 0 {
			dropped++
			continue
		}
		sb.WriteString(line.String())
		used += line.Len()
		ids = append(ids, t.ID)
	}

	if dropped > 0 {
		fmt.Fprintf(&sb, "\n%d further rule(s) did not fit this budget. "+
			"List them all with `saga rules`.\n", dropped)
	}

	return strings.TrimRight(sb.String(), "\n"), ids, nil
}

// notesByScopesAndType is notesByScopeAndType across several scopes, ordered
// deterministically so the rendered output is stable between invocations.
func (s *Service) notesByScopesAndType(scopes, types []string) ([]*Topic, error) {
	var all []*Topic
	for _, sc := range scopes {
		notes, err := s.notesByScopeAndType(sc, types)
		if err != nil {
			return nil, err
		}
		all = append(all, notes...)
	}
	return all, nil
}
