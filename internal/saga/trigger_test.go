package saga

import "testing"

func TestMatchTrigger(t *testing.T) {
	cases := []struct {
		name       string
		pattern    string
		actionName string
		actionArg  string
		want       bool
	}{
		{"bare name matches any argument", "Bash", "Bash", "anything at all", true},
		{"bare name rejects other action", "Bash", "Edit", "", false},
		{"name is case sensitive", "bash", "Bash", "", false},
		{"prefix glob", "Bash(git commit*)", "Bash", "git commit -m 'x'", true},
		{"prefix glob is anchored at the start", "Bash(git commit*)", "Bash", "sudo git commit", false},
		{"suffix glob", "Edit(*.ts)", "Edit", "/src/app.ts", true},
		{"suffix glob rejects other extension", "Edit(*.ts)", "Edit", "/src/app.go", false},
		{"middle glob", "Bash(git *--force*)", "Bash", "git push --force origin", true},
		{"exact argument, no glob", "Bash(git status)", "Bash", "git status", true},
		{"exact argument rejects extra", "Bash(git status)", "Bash", "git status -s", false},
		{"lone star matches everything", "Bash(*)", "Bash", "rm -rf /", true},
		{"lone star matches empty argument", "Bash(*)", "Bash", "", true},
		{"empty argument against a real pattern", "Bash(git *)", "Bash", "", false},
		{"whitespace around name is tolerated", " Bash (git *)", "Bash", "git log", true},
		// Host-neutrality: the engine matches strings, it does not know what an
		// action means. A clinical host's namespace must work identically.
		{"non-dev namespace", "emr.write(patient/*)", "emr.write", "patient/4711", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchTrigger(tc.pattern, tc.actionName, tc.actionArg); got != tc.want {
				t.Errorf("MatchTrigger(%q, %q, %q) = %v, want %v",
					tc.pattern, tc.actionName, tc.actionArg, got, tc.want)
			}
		})
	}
}

func triggeredNote(id, scope, title, enforcement string, triggers []string) string {
	s := "---\nid: " + id + "\nscope: " + scope + "\ntype: policy\ntitle: " + title + "\n"
	if enforcement != "" {
		s += "enforcement: " + enforcement + "\n"
	}
	if len(triggers) > 0 {
		s += "triggers:\n"
		for _, tr := range triggers {
			s += "  - \"" + tr + "\"\n"
		}
	}
	return s + "created_at: 2026-04-12T10:30:00Z\nupdated_at: 2026-04-12T10:30:00Z\n---\n\nthe rule body\n"
}

func TestRulesForAction(t *testing.T) {
	svc, db := setupServiceTest(t)

	layer := setupProjectLayer(t, "personal", map[string]string{
		"commit.md": triggeredNote("01HXY5KZQVJ8M3R7ABCDEFGH01", "personal",
			"Git identity", "", []string{"Bash(git commit*)", "Bash(git push*)"}),
		"secrets.md": triggeredNote("01HXY5KZQVJ8M3R7ABCDEFGH02", "personal",
			"Never commit secrets", "block", []string{"Bash(git commit*)"}),
		"typescript.md": triggeredNote("01HXY5KZQVJ8M3R7ABCDEFGH03", "personal",
			"TS conventions", "", []string{"Edit(*.ts)"}),
		"untriggered.md": triggeredNote("01HXY5KZQVJ8M3R7ABCDEFGH04", "personal",
			"A rule claiming no action", "", nil),
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("index: %v", err)
	}

	rules, err := svc.RulesForAction("Bash", "git commit -m 'wip'")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("matched %d rules, want 2: %+v", len(rules), rules)
	}
	// Blocking rules must come first so a caller acting on the first match
	// cannot silently miss a hard rule.
	if rules[0].Enforcement != EnforcementBlock {
		t.Errorf("blocking rule not first: %+v", rules)
	}
	if rules[1].Enforcement != EnforcementAdvise {
		t.Errorf("second rule should default to advise, got %q", rules[1].Enforcement)
	}

	// A rule with no triggers has claimed no action and must never fire.
	for _, r := range rules {
		if r.Title == "A rule claiming no action" {
			t.Error("a topic with no triggers was injected into an action")
		}
	}

	// Non-matching action yields nothing at all — the guard must be free when
	// no rule applies.
	none, err := svc.RulesForAction("WebFetch", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("unrelated action matched %d rules: %+v", len(none), none)
	}

	edits, err := svc.RulesForAction("Edit", "/src/app.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].Title != "TS conventions" {
		t.Errorf("Edit(*.ts) matching failed: %+v", edits)
	}
}
