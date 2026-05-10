package saga

import "testing"

func TestDetectSecrets_awsAccessKey(t *testing.T) {
	hits := DetectSecrets("Set the env: AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\nThen run.")
	if len(hits) != 1 || hits[0].Kind != "aws_access_key" {
		t.Fatalf("expected 1 aws_access_key hit, got %+v", hits)
	}
	if hits[0].Line != 1 {
		t.Errorf("line = %d, want 1", hits[0].Line)
	}
}

func TestDetectSecrets_sshPrivateKey(t *testing.T) {
	body := "Investigation\n\n-----BEGIN OPENSSH PRIVATE KEY-----\nbase64junk\n-----END OPENSSH PRIVATE KEY-----\n"
	hits := DetectSecrets(body)
	if len(hits) == 0 {
		t.Fatal("expected ssh_private_key hit")
	}
	found := false
	for _, h := range hits {
		if h.Kind == "ssh_private_key" {
			found = true
		}
	}
	if !found {
		t.Errorf("ssh_private_key not detected: %+v", hits)
	}
}

func TestDetectSecrets_jwt(t *testing.T) {
	body := "the auth header was: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	hits := DetectSecrets(body)
	if len(hits) == 0 {
		t.Fatal("expected jwt hit")
	}
	if hits[0].Kind != "jwt_token" {
		t.Errorf("kind = %q, want jwt_token", hits[0].Kind)
	}
}

func TestDetectSecrets_connectionString(t *testing.T) {
	body := "DATABASE_URL=postgres://user:p4ssw0rd@db.internal:5432/app"
	hits := DetectSecrets(body)
	found := false
	for _, h := range hits {
		if h.Kind == "connection_string_with_password" {
			found = true
		}
	}
	if !found {
		t.Errorf("connection_string_with_password not detected: %+v", hits)
	}
}

func TestDetectSecrets_falsePositiveControlNaturalProse(t *testing.T) {
	// Topic body that talks ABOUT credentials without containing real ones.
	body := `# How we handle credentials

Use AWS IAM roles instead of static keys. Avoid committing tokens to git.
Reset password if compromised. Set DATABASE_URL via env, not in source.`
	hits := DetectSecrets(body)
	if len(hits) > 0 {
		t.Errorf("natural prose should not match secret patterns; got %+v", hits)
	}
}

func TestDetectSecrets_falsePositiveControlExampleUrls(t *testing.T) {
	body := `Visit https://example.com or http://localhost:8080. See docs at https://github.com/mopanc/saga.`
	hits := DetectSecrets(body)
	for _, h := range hits {
		if h.Kind == "connection_string_with_password" {
			t.Errorf("plain https URL incorrectly flagged: %+v", h)
		}
	}
}

func TestDetectSecrets_emptyBody(t *testing.T) {
	if hits := DetectSecrets(""); len(hits) != 0 {
		t.Errorf("empty body: got %+v, want []", hits)
	}
}
