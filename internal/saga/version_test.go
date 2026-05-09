package saga

import "testing"

func TestVersionStringStripsLeadingV(t *testing.T) {
	t.Cleanup(func() { Version = "0.2.0-dev" })

	cases := []struct {
		in, want string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v1.0.0-beta.2", "1.0.0-beta.2"},
	}
	for _, c := range cases {
		Version = c.in
		if got := VersionString(); got != c.want {
			t.Errorf("VersionString() with Version=%q = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVersionStringDevFallback(t *testing.T) {
	// With Version at the dev placeholder and no useful BuildInfo (this test
	// binary's Main.Version is "(devel)"), VersionString returns the dev string.
	t.Cleanup(func() { Version = "0.2.0-dev" })
	Version = "0.2.0-dev"
	if got := VersionString(); got != "0.2.0-dev" {
		t.Errorf("VersionString() dev fallback = %q, want %q", got, "0.2.0-dev")
	}
}
