package validate

import (
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

func tcpChall(t *testing.T, root, cat, slug string, port int) *challenge.Challenge {
	t.Helper()
	c := makeChallenge(t, root, cat, slug, "", "")
	p := port
	c.Manifest.Deploy.Port = &p
	c.Manifest.Deploy.TCP = true
	return c
}

func rulesOf(r *Report) []string {
	var out []string
	for _, i := range r.Issues {
		out = append(out, i.Rule)
	}
	return out
}

func TestCheckKubernetes_NoopOutsideKubernetesMode(t *testing.T) {
	cfg := k8sCfg(nil)
	cfg.Deploy.Mode = "docker"
	c := tcpChall(t, t.TempDir(), "pwn", "bad_slug", 15000)
	r := &Report{}
	checkKubernetes(r, cfg, "/", []*challenge.Challenge{c})
	if len(r.Issues) != 0 {
		t.Fatalf("expected no issues in docker mode, got %v", rulesOf(r))
	}
}

func TestCheckKubernetes_Clean(t *testing.T) {
	root := t.TempDir()
	a := tcpChall(t, root, "pwn", "a", 11001)
	b := tcpChall(t, root, "pwn", "b", 11002)
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{a, b})
	if len(r.Issues) != 0 {
		t.Fatalf("unexpected issues: %v", rulesOf(r))
	}
}

func TestCheckKubernetes_K001_PortOutsideCategory(t *testing.T) {
	root := t.TempDir()
	c := tcpChall(t, root, "pwn", "a", 15000)
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{c})
	if got := rulesOf(r); len(got) != 1 || got[0] != "K001" {
		t.Fatalf("rules = %v, want [K001]", got)
	}
}

func TestCheckKubernetes_K001_TCPWithoutPort(t *testing.T) {
	root := t.TempDir()
	c := tcpChall(t, root, "pwn", "a", 11001)
	c.Manifest.Deploy.Port = nil
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{c})
	if got := rulesOf(r); len(got) != 1 || got[0] != "K001" {
		t.Fatalf("rules = %v, want [K001]", got)
	}
}

func TestCheckKubernetes_K002_BadName(t *testing.T) {
	root := t.TempDir()
	c := tcpChall(t, root, "pwn", "My_Chall", 11001)
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{c})
	got := rulesOf(r)
	if len(got) != 1 || got[0] != "K002" {
		t.Fatalf("rules = %v, want [K002]", got)
	}
	if !strings.Contains(r.Issues[0].Message, "DNS-1123") {
		t.Errorf("message = %q", r.Issues[0].Message)
	}
}

func TestCheckKubernetes_K002_NameTooLong(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "web", strings.Repeat("x", 60), "", "")
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{c})
	if got := rulesOf(r); len(got) != 1 || got[0] != "K002" {
		t.Fatalf("rules = %v, want [K002]", got)
	}
}

func TestCheckKubernetes_K003_NodePortCollision(t *testing.T) {
	root := t.TempDir()
	// pwn 11001 and jailbreak 16001 both have offset 1 -> NodePort 30001.
	a := tcpChall(t, root, "pwn", "a", 11001)
	b := tcpChall(t, root, "jailbreak", "b", 16001)
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{a, b})
	if got := rulesOf(r); len(got) != 1 || got[0] != "K003" {
		t.Fatalf("rules = %v, want [K003]", got)
	}
}

func TestCheckKubernetes_HTTPChallengeNeedsNoNodePort(t *testing.T) {
	root := t.TempDir()
	c := makeChallenge(t, root, "web", "site", "", "")
	p := 20000 // out of range, but irrelevant for non-TCP
	c.Manifest.Deploy.Port = &p
	r := &Report{}
	checkKubernetes(r, k8sCfg(nil), root, []*challenge.Challenge{c})
	if len(r.Issues) != 0 {
		t.Fatalf("unexpected issues: %v", rulesOf(r))
	}
}
