package saga

import (
	"strings"
	"testing"
)

// TestBuildRuleCatalog_listsPoliciesNotBodies locks in the design: the
// always-on block carries pointers to every rule in force, not the rules
// themselves. Injecting the bodies costs ~6.3k tokens on the personal layer
// alone, on every prompt.
func TestBuildRuleCatalog_listsPoliciesNotBodies(t *testing.T) {
	svc, _ := setupServiceTest(t)

	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "git-identity", Scope: "personal", Type: "policy",
		Title:    "Git identity — personal vs work",
		Synonyms: []string{"git email", "commit author"},
		Body:     "SECRET-BODY-MARKER: personal commits use the personal email.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "no-quantum", Scope: "personal", Type: "policy",
		Title: "No quantum positioning", Body: "Do not position with quantum computing.",
	}); err != nil {
		t.Fatal(err)
	}
	// A non-policy note must not appear in the catalogue.
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "some-topic", Scope: "personal", Type: "topic",
		Title: "An investigation", Body: "not a rule",
	}); err != nil {
		t.Fatal(err)
	}

	out, ids, err := svc.BuildRuleCatalog(400)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("contributing ids = %d, want 2 policies", len(ids))
	}
	if !strings.Contains(out, "Git identity — personal vs work") {
		t.Error("policy title missing from catalogue")
	}
	if !strings.Contains(out, "git email") {
		t.Error("synonyms missing — they are the trigger phrases the model matches on")
	}
	if strings.Contains(out, "SECRET-BODY-MARKER") {
		t.Error("catalogue leaked a policy body; it must carry pointers only")
	}
	if strings.Contains(out, "An investigation") {
		t.Error("non-policy note appeared in the rule catalogue")
	}
	if !strings.Contains(out, "topic_read") {
		t.Error("catalogue must tell the model how to fetch the full rule")
	}
}

// TestBuildRuleCatalog_includesProposedConfidence guards the selector choice.
// The original #87 proposal suggested selecting on `confidence: canonical`;
// every policy note in the field carries the parser default `proposed`, so
// that selector would inject nothing at all.
func TestBuildRuleCatalog_includesProposedConfidence(t *testing.T) {
	svc, db := setupServiceTest(t)

	r, err := svc.TopicWrite(TopicWriteArgs{
		Name: "a-rule", Scope: "personal", Type: "policy",
		Title: "A proposed rule", Body: "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	var confidence string
	if err := db.QueryRow("SELECT confidence FROM topic_index WHERE id = ?", r.ID).Scan(&confidence); err != nil {
		t.Fatal(err)
	}
	if confidence == "canonical" {
		t.Fatalf("setup assumption broken: default confidence is %q", confidence)
	}

	out, ids, err := svc.BuildRuleCatalog(400)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || !strings.Contains(out, "A proposed rule") {
		t.Errorf("a proposed-confidence policy was excluded from the catalogue: %q", out)
	}
}

func TestBuildRuleCatalog_emptyWhenNoPolicies(t *testing.T) {
	svc, _ := setupServiceTest(t)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "x", Scope: "personal", Type: "topic", Body: "body",
	}); err != nil {
		t.Fatal(err)
	}
	out, ids, err := svc.BuildRuleCatalog(400)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" || ids != nil {
		t.Errorf("expected empty catalogue, got %q / %v", out, ids)
	}
}

// TestBuildRuleCatalog_overflowDropsWholeEntries is the regression for a bug
// caught by running the hook against a real store: character-boundary
// truncation cut at the last paragraph break, which sits directly under the
// header, so a 10-rule catalogue rendered as a header with no rules under it.
// Overflow must drop whole entries and say how many.
func TestBuildRuleCatalog_overflowDropsWholeEntries(t *testing.T) {
	svc, _ := setupServiceTest(t)

	for _, n := range []string{"rule-a", "rule-b", "rule-c", "rule-d", "rule-e"} {
		if _, err := svc.TopicWrite(TopicWriteArgs{
			Name: n, Scope: "personal", Type: "policy",
			Title:    "Rule " + n + " " + strings.Repeat("padding ", 10),
			Synonyms: []string{strings.Repeat("trigger phrase ", 8)},
			Body:     "body",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// A budget that cannot hold all five entries.
	out, ids, err := svc.BuildRuleCatalog(120)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("catalogue dropped every rule; at least one must survive")
	}
	if len(ids) == 5 {
		t.Fatal("setup did not overflow the budget; test proves nothing")
	}
	if !strings.Contains(out, "did not fit this budget") {
		t.Error("silent truncation: overflow must be reported, not hidden")
	}
	// Every emitted entry must be a whole line, never a half-rendered one.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- **") && !strings.Contains(line, "`[") {
			t.Errorf("entry was cut mid-render: %q", line)
		}
	}
}

// TestBuildRuleCatalog_excludesTriggeredRules locks in the efficiency property:
// a rule bound to an action is delivered by the guard at the moment that action
// happens, so listing it in the always-on catalogue too is redundant spend on
// every prompt. The always-on cost therefore falls as rules gain triggers,
// instead of growing with the store.
func TestBuildRuleCatalog_excludesTriggeredRules(t *testing.T) {
	svc, db := setupServiceTest(t)

	layer := setupProjectLayer(t, "personal", map[string]string{
		"triggered.md": triggeredNote("01HXY5KZQVJ8M3R7ABCDEFGH01", "personal",
			"Bound to git commits", "", []string{"Bash(git commit*)"}),
		"general.md": triggeredNote("01HXY5KZQVJ8M3R7ABCDEFGH02", "personal",
			"Not bound to any action", "", nil),
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatal(err)
	}

	out, ids, err := svc.BuildRuleCatalog(700)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("catalogue listed %d rules, want 1 (the untriggered one)", len(ids))
	}
	if strings.Contains(out, "Bound to git commits") {
		t.Error("a triggered rule was listed in the catalogue as well as being guard-delivered")
	}
	if !strings.Contains(out, "Not bound to any action") {
		t.Error("the untriggered rule must still be listed; nothing else delivers it")
	}
	// Silence about the omitted rules would read as "these are all your rules".
	if !strings.Contains(out, "bound to specific actions") {
		t.Error("catalogue must account for the rules it deliberately omitted")
	}
}
