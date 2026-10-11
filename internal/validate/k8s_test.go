package validate

import (
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func intPtr(i int) *int { return &i }

func k8sCfg(base *int) *config.Config {
	return &config.Config{
		Event: config.Event{Slug: "evt"},
		Deploy: &config.Deploy{
			Mode:       "kubernetes",
			Kubernetes: &config.KubernetesDeploy{NodePortBase: base},
		},
	}
}

func TestNodePortFor(t *testing.T) {
	tests := []struct {
		name     string
		base     *int
		category string
		port     int
		want     int
		wantErr  string
	}{
		{"default base, first port of pwn", nil, "pwn", 11000, 30000, ""},
		{"default base, offset 500", nil, "pwn", 11500, 30500, ""},
		{"default base, last port of pwn", nil, "pwn", 11999, 30999, ""},
		{"web first port", nil, "web", 10000, 30000, ""},
		{"explicit base 31000", intPtr(31000), "pwn", 11001, 31001, ""},
		{"base 0 passes port through", intPtr(0), "pwn", 11500, 11500, ""},
		{"base 0 rejects invalid port", intPtr(0), "pwn", 70000, 0, "not a valid TCP port"},
		{"port outside category range", nil, "pwn", 15000, 0, "outside pwn range"},
		{"unknown category", nil, "nope", 11000, 0, "no port range"},
		{"category without range", nil, "forensics", 11000, 0, "no port range"},
		{"derived port above 32767", intPtr(32500), "pwn", 11999, 0, "outside 30000-32767"},
		{"derived port below 30000", intPtr(29000), "pwn", 11000, 0, "outside 30000-32767"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NodePortFor(k8sCfg(tt.base), tt.category, tt.port)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("NodePortFor = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNodePortFor_Deterministic(t *testing.T) {
	cfg := k8sCfg(nil)
	a, _ := NodePortFor(cfg, "pwn", 11234)
	b, _ := NodePortFor(cfg, "pwn", 11234)
	if a != b {
		t.Fatalf("not deterministic: %d vs %d", a, b)
	}
}

func TestCheckK8sName(t *testing.T) {
	long := "mctf-" + strings.Repeat("a", 60)
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", "mctf-evt-web-hello", false},
		{"exactly 63", "a" + strings.Repeat("b", 62), false},
		{"64 chars", "a" + strings.Repeat("b", 63), true},
		{"too long", long, true},
		{"uppercase", "mctf-Evt-web-x", true},
		{"underscore", "mctf-evt-web-my_chall", true},
		{"leading dash", "-mctf", true},
		{"trailing dash", "mctf-", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckK8sName(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckK8sName(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestK8sResourceName(t *testing.T) {
	if got := K8sResourceName("evt", "pwn", "baby"); got != "mctf-evt-pwn-baby" {
		t.Errorf("got %q", got)
	}
}
