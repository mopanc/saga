package saga

import "testing"

func TestSanitizeFTSQuery(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"mjpeg performance", `"mjpeg" OR "performance"`},
		{"MJPEG-performance", `"MJPEG" OR "performance"`}, // hyphen splits, matches stored tokens
		{"x*y(z)", `"x" OR "y" OR "z"`},
		{"***", ""},
		{"", ""},
		{"AND not OR NEAR", ""}, // all keywords dropped
		{"go AND saga", `"go" OR "saga"`},
		{"  spaces   ", `"spaces"`},
		{"memória", `"memória"`}, // unicode letters preserved
		{"go2rtc-arch", `"go2rtc" OR "arch"`},
	}
	for _, tc := range cases {
		got := sanitizeFTSQuery(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
