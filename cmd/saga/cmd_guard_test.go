package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestActionArgument(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bash command", `{"command":"git commit -m x","description":"commit"}`, "git commit -m x"},
		{"edit file path", `{"file_path":"/src/app.ts","old_string":"a"}`, "/src/app.ts"},
		{"generic path", `{"path":"/tmp/x"}`, "/tmp/x"},
		{"url", `{"url":"https://example.com"}`, "https://example.com"},
		{"no recognised field", `{"pattern":"*.go","glob":true}`, ""},
		{"empty input", ``, ""},
		{"malformed json degrades to empty", `{not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionArgument(json.RawMessage(tc.input)); got != tc.want {
				t.Errorf("actionArgument(%s) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestEmitGuard_shapeMatchesHookContract locks the exact JSON the PreToolUse
// contract expects. A silently wrong field name would make every rule injection
// a no-op with no visible failure.
func TestEmitGuard_shapeMatchesHookContract(t *testing.T) {
	var buf bytes.Buffer
	emitGuard(&buf, "some rule text", "", "")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("guard output is not valid JSON: %v", err)
	}
	hso, ok := parsed["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatal("missing hookSpecificOutput object")
	}
	if hso["hookEventName"] != "PreToolUse" {
		t.Errorf("hookEventName = %v, want PreToolUse", hso["hookEventName"])
	}
	if hso["additionalContext"] != "some rule text" {
		t.Errorf("additionalContext = %v", hso["additionalContext"])
	}
	// An advisory injection must not carry a permission decision: emitting an
	// empty decision would be read as a verdict.
	if _, present := hso["permissionDecision"]; present {
		t.Error("advisory guard output must omit permissionDecision entirely")
	}
}

func TestEmitGuard_denyCarriesReason(t *testing.T) {
	var buf bytes.Buffer
	emitGuard(&buf, "", "deny", "Blocked by rule X")

	var parsed struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
			AdditionalContext        string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("permissionDecision = %q, want deny", parsed.HookSpecificOutput.PermissionDecision)
	}
	if parsed.HookSpecificOutput.PermissionDecisionReason == "" {
		t.Error("a denial with no reason leaves the agent nothing to act on")
	}
}
