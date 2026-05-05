package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeExe = "/usr/local/bin/saga"

func TestApplyHookRegistration_freshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := applyHookRegistration(fakeExe, path); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !hookFilePresent(t, path, fakeExe+" hook") {
		t.Errorf("hook not present after fresh write")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("expected no .bak for fresh file, got: %v", err)
	}
}

func TestApplyHookRegistration_idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := applyHookRegistration(fakeExe, path); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first, _ := os.ReadFile(path)

	if err := applyHookRegistration(fakeExe, path); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	second, _ := os.ReadFile(path)

	if string(first) != string(second) {
		t.Errorf("non-idempotent — second apply mutated the file:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestApplyHookRegistration_preservesUnrelatedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	original := map[string]any{
		"theme": "dark",
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "/path/to/other"},
		},
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/some/other/hook"}}},
			},
		},
		"customField": "must-survive",
	}
	writeJSON(t, path, original)

	if err := applyHookRegistration(fakeExe, path); err != nil {
		t.Fatalf("apply: %v", err)
	}

	merged := readJSON(t, path)
	if merged["theme"] != "dark" {
		t.Errorf("theme lost: %v", merged["theme"])
	}
	if merged["customField"] != "must-survive" {
		t.Errorf("customField lost: %v", merged["customField"])
	}
	mcps, _ := merged["mcpServers"].(map[string]any)
	if _, ok := mcps["other"]; !ok {
		t.Errorf("mcpServers.other lost: %v", mcps)
	}
	hooks, _ := merged["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"].([]any); !ok {
		t.Errorf("PreToolUse lost: %v", hooks)
	}
	if !hookFilePresent(t, path, fakeExe+" hook") {
		t.Errorf("saga hook not added")
	}
}

func TestApplyHookRegistration_preservesPeerUserPromptSubmitHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	original := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "/some/other/tool prompt-hook"},
					},
				},
			},
		},
	}
	writeJSON(t, path, original)

	if err := applyHookRegistration(fakeExe, path); err != nil {
		t.Fatalf("apply: %v", err)
	}

	merged := readJSON(t, path)
	hooks := merged["hooks"].(map[string]any)
	groups := hooks["UserPromptSubmit"].([]any)
	if len(groups) < 2 {
		t.Fatalf("expected ≥2 UserPromptSubmit groups (peer + saga), got %d", len(groups))
	}

	// The original peer hook must still be there.
	if !hookFilePresent(t, path, "/some/other/tool prompt-hook") {
		t.Errorf("peer UserPromptSubmit hook lost")
	}
	if !hookFilePresent(t, path, fakeExe+" hook") {
		t.Errorf("saga hook not added")
	}
}

func TestApplyHookRegistration_writesBackupBeforeMutating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeJSON(t, path, map[string]any{"theme": "dark"})

	if err := applyHookRegistration(fakeExe, path); err != nil {
		t.Fatalf("apply: %v", err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	var bakCfg map[string]any
	if err := json.Unmarshal(bak, &bakCfg); err != nil {
		t.Fatalf("backup invalid JSON: %v", err)
	}
	if bakCfg["theme"] != "dark" {
		t.Errorf("backup does not match pre-merge content: %v", bakCfg)
	}
	if _, hasHooks := bakCfg["hooks"]; hasHooks {
		t.Errorf("backup should be the PRE-merge file (no hooks key), got: %v", bakCfg)
	}
}

func TestApplyHookRegistration_refusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{ not-valid-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)

	err := applyHookRegistration(fakeExe, path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("expected 'valid JSON' in error, got: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(original) != string(after) {
		t.Errorf("file was modified despite invalid JSON; want pristine, got: %s", after)
	}
}

func TestHookAlreadyWired(t *testing.T) {
	cfg := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "/path/to/saga hook"},
					},
				},
			},
		},
	}
	if !hookAlreadyWired(cfg, "saga", "hook") {
		t.Error("expected wired=true")
	}
	if hookAlreadyWired(cfg, "nonexistent") {
		t.Error("expected wired=false for nonexistent substring")
	}
	if hookAlreadyWired(map[string]any{}, "saga") {
		t.Error("expected wired=false for empty config")
	}
}

// --- helpers ---

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("read %s: %v\ncontent:\n%s", path, err, data)
	}
	return out
}

func hookFilePresent(t *testing.T, path, mustContain string) bool {
	t.Helper()
	cfg := readJSON(t, path)
	return hookAlreadyWired(cfg, mustContain)
}
