package kubernetes

import (
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func tcpUnitChall(t *testing.T, root, cat, slug string, port int) *challenge.Challenge {
	t.Helper()
	c := newServiceChall(t, root, cat, slug, port, 4444, slug)
	c.Manifest.Deploy.TCP = true
	return c
}

func TestPlan_TCPNodePortIsDerivedFromCategoryOffset(t *testing.T) {
	root := t.TempDir()
	cfg := cfgK8s("ns", "nginx")
	plan, err := Plan(cfg, []*challenge.Challenge{tcpUnitChall(t, root, "pwn", "baby", 11042)}, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan[0].NodePort; got != 30042 {
		t.Errorf("NodePort = %d, want 30042", got)
	}
	raw, err := ManifestsYAML(plan[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "nodePort: 30042") {
		t.Errorf("manifest missing nodePort: 30042:\n%s", raw)
	}
}

func TestPlan_NodePortBaseConfigurable(t *testing.T) {
	root := t.TempDir()
	cfg := cfgK8s("ns", "nginx")
	base := 31000
	cfg.Deploy.Kubernetes.NodePortBase = &base
	plan, err := Plan(cfg, []*challenge.Challenge{tcpUnitChall(t, root, "pwn", "baby", 11005)}, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan[0].NodePort; got != 31005 {
		t.Errorf("NodePort = %d, want 31005", got)
	}
}

func TestPlan_NodePortBaseZeroUsesPortVerbatim(t *testing.T) {
	root := t.TempDir()
	cfg := cfgK8s("ns", "nginx")
	zero := 0
	cfg.Deploy.Kubernetes.NodePortBase = &zero
	plan, err := Plan(cfg, []*challenge.Challenge{tcpUnitChall(t, root, "pwn", "baby", 11005)}, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan[0].NodePort; got != 11005 {
		t.Errorf("NodePort = %d, want 11005", got)
	}
}

func TestPlan_HTTPChallengeHasNoNodePort(t *testing.T) {
	root := t.TempDir()
	c := newServiceChall(t, root, "web", "site", 10001, 80, "site")
	plan, err := Plan(cfgK8s("ns", "nginx"), []*challenge.Challenge{c}, Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan[0].NodePort != 0 {
		t.Errorf("NodePort = %d, want 0", plan[0].NodePort)
	}
}

func TestPlan_NodePortErrors(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name    string
		challs  func() []*challenge.Challenge
		cfg     func() *config.Config
		wantErr string
	}{
		{
			name: "port outside category range",
			challs: func() []*challenge.Challenge {
				return []*challenge.Challenge{tcpUnitChall(t, root, "pwn", "a", 15000)}
			},
			wantErr: "outside pwn range",
		},
		{
			name: "tcp without port",
			challs: func() []*challenge.Challenge {
				c := tcpUnitChall(t, root, "pwn", "b", 11001)
				c.Manifest.Deploy.Port = nil
				return []*challenge.Challenge{c}
			},
			wantErr: "requires deploy.port",
		},
		{
			name: "nodeport collision across categories",
			challs: func() []*challenge.Challenge {
				return []*challenge.Challenge{
					tcpUnitChall(t, root, "pwn", "c", 11007),
					tcpUnitChall(t, root, "jailbreak", "d", 16007),
				}
			},
			wantErr: "already assigned",
		},
		{
			name: "name too long",
			challs: func() []*challenge.Challenge {
				return []*challenge.Challenge{newServiceChall(t, root, "web", strings.Repeat("z", 60), 10001, 80, "z")}
			},
			wantErr: "max 63",
		},
		{
			name: "name not DNS-1123",
			challs: func() []*challenge.Challenge {
				return []*challenge.Challenge{newServiceChall(t, root, "web", "Bad_Slug", 10001, 80, "z")}
			},
			wantErr: "DNS-1123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Plan(cfgK8s("ns", "nginx"), tt.challs(), Options{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
