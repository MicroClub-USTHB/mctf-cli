package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmYes(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"  yes  \n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"maybe\n", false},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		got := confirmYes(strings.NewReader(tc.in), &out, "delete?")
		if got != tc.want {
			t.Errorf("confirmYes(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if !strings.Contains(out.String(), "delete? [y/N]") {
			t.Errorf("prompt should appear in writer, got: %q", out.String())
		}
	}
}
