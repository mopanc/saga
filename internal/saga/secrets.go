package saga

import (
	"regexp"
	"strings"
)

// SecretPattern names a class of credential we refuse to persist by default.
// Keep the list small and high-signal — false positives in topic prose are
// worse than the rare secret that slips through, because the AllowSecret
// escape hatch always exists.
type SecretPattern struct {
	Kind    string
	Pattern *regexp.Regexp
}

// SecretMatch — what we found and where, in 1-based line numbers.
type SecretMatch struct {
	Kind string `json:"kind"`
	Line int    `json:"line"`
}

// secretPatterns — high-signal credential matchers. Calibrated for low false
// positive rate over natural-language topic content. Source-controllable so
// `saga doctor` (future) can render the same list.
var secretPatterns = []SecretPattern{
	{
		// AWS Access Key ID. AKIA = root/IAM, ASIA = STS temporary.
		Kind:    "aws_access_key",
		Pattern: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`),
	},
	{
		// SSH/PGP/RSA private key block header. Any flavour.
		Kind:    "ssh_private_key",
		Pattern: regexp.MustCompile(`-----BEGIN ((RSA|OPENSSH|EC|DSA|PGP) )?PRIVATE KEY( BLOCK)?-----`),
	},
	{
		// GCP service account JSON. Pin on the literal "type":"service_account"
		// pair which is unique to credentials.json files.
		Kind:    "gcp_service_account",
		Pattern: regexp.MustCompile(`"type"\s*:\s*"service_account"`),
	},
	{
		// JWT — three base64url segments separated by dots, total ≥40 chars.
		Kind:    "jwt_token",
		Pattern: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	},
	{
		// GitHub fine-grained / classic PATs.
		Kind:    "github_token",
		Pattern: regexp.MustCompile(`\bgh[opsu]_[A-Za-z0-9]{36,}\b`),
	},
	{
		// Slack tokens (xoxb-/xoxp-/xoxa-/xoxr-).
		Kind:    "slack_token",
		Pattern: regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	},
	{
		// Connection string with embedded password: scheme://user:secret@host
		// Excludes the common `https://example.com` URL by requiring user:pass.
		Kind:    "connection_string_with_password",
		Pattern: regexp.MustCompile(`\b[a-z]{2,}\+?[a-z]*://[^\s:/@]+:[^\s/@]+@[^\s/]+`),
	},
}

// DetectSecrets scans body for credential-shaped patterns. Returns the list
// of matches with 1-based line numbers. Non-blocking on its own — callers
// decide whether to refuse the write (TopicWrite does, by default).
func DetectSecrets(body string) []SecretMatch {
	if body == "" {
		return nil
	}
	var hits []SecretMatch
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		for _, p := range secretPatterns {
			if p.Pattern.MatchString(line) {
				hits = append(hits, SecretMatch{Kind: p.Kind, Line: i + 1})
			}
		}
	}
	return hits
}
