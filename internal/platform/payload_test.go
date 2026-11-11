package platform

import (
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func TestResolveScoring_DefaultsStatic100(t *testing.T) {
	got := ResolveScoring(nil, nil)
	want := Scoring{Type: "static", Points: 100}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveScoring_EventDynamicDefaults(t *testing.T) {
	cfg := &config.Config{Event: config.Event{Scoring: &config.Scoring{Type: "dynamic"}}}
	got := ResolveScoring(cfg, nil)
	want := Scoring{Type: "dynamic", Initial: 500, Decay: 10, Minimum: 100}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveScoring_OverrideWins(t *testing.T) {
	cfg := &config.Config{Event: config.Event{Scoring: &config.Scoring{Type: "dynamic", Initial: 400}}}
	got := ResolveScoring(cfg, &challenge.Scoring{Type: "static", Points: 250})
	want := Scoring{Type: "static", Points: 250}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestConnectionInfo(t *testing.T) {
	port := func(p int) *int { return &p }
	domainCfg := &config.Config{Event: config.Event{Domain: "mctf.example"}}
	service := func(d challenge.DeployBlock) *challenge.Challenge {
		return &challenge.Challenge{
			Archetype: challenge.ArchetypeService,
			Manifest: challenge.Manifest{
				Challenge: challenge.ChallengeBlock{NeedDeploy: true},
				Deploy:    &d,
			},
		}
	}
	instanced := &challenge.Challenge{
		Archetype: challenge.ArchetypeInstanced,
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{NeedDeploy: true},
			Deploy:    &challenge.DeployBlock{Subdomain: "inst"},
		},
	}

	cases := []struct {
		name string
		cfg  *config.Config
		c    *challenge.Challenge
		want string
	}{
		{"static", domainCfg, &challenge.Challenge{Archetype: challenge.ArchetypeStatic}, ""},
		{"instanced explicit url", &config.Config{Instancer: &config.Instancer{URL: "https://inst.example"}}, instanced, "https://inst.example"},
		{"instanced from domain", domainCfg, instanced, "https://instancer.mctf.example"},
		{"service https", domainCfg, service(challenge.DeployBlock{Subdomain: "web1"}), "https://web1.mctf.example"},
		{"service no ssl", domainCfg, service(challenge.DeployBlock{Subdomain: "web1", NoSSL: true}), "http://web1.mctf.example"},
		{"tcp", domainCfg, service(challenge.DeployBlock{Subdomain: "web1", TCP: true, Port: port(31337)}), "nc web1.mctf.example 31337"},
		{"tcp no port", domainCfg, service(challenge.DeployBlock{Subdomain: "web1", TCP: true}), ""},
		{"explicit port", domainCfg, service(challenge.DeployBlock{Subdomain: "web1", Port: port(8443)}), "https://web1.mctf.example:8443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConnectionInfo(tc.cfg, tc.c); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescriptionWithAuthor(t *testing.T) {
	if got := DescriptionWithAuthor("desc\n\n", "bob"); got != "desc\n\n**Author: bob**" {
		t.Errorf("with author: got %q", got)
	}
	if got := DescriptionWithAuthor("desc\n", ""); got != "desc" {
		t.Errorf("no author: got %q", got)
	}
}
