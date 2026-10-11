package cmd

import (
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

func instanced(slug string, port int) *challenge.Challenge {
	p := port
	return &challenge.Challenge{
		Slug:      slug,
		Archetype: challenge.ArchetypeInstanced,
		Manifest:  challenge.Manifest{Deploy: &challenge.DeployBlock{InternalPort: &p}},
	}
}

func TestInstancedCount(t *testing.T) {
	challs := []*challenge.Challenge{
		instanced("a", 8001),
		instanced("b", 8002),
		{Slug: "static", Archetype: challenge.ArchetypeStatic},                                                                     // not instanced
		{Slug: "noport", Archetype: challenge.ArchetypeInstanced},                                                                  // no deploy block
		{Slug: "nilport", Archetype: challenge.ArchetypeInstanced, Manifest: challenge.Manifest{Deploy: &challenge.DeployBlock{}}}, // nil port
	}
	if got := instancedCount(challs); got != 2 {
		t.Errorf("instancedCount = %d, want 2", got)
	}
	if got := instancedCount(nil); got != 0 {
		t.Errorf("instancedCount(nil) = %d, want 0", got)
	}
}

func TestMonitoringCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"monitoring", "up"})
	if err != nil {
		t.Fatalf("monitoring up not registered: %v", err)
	}
	if cmd.Name() != "up" {
		t.Errorf("resolved command = %q, want up", cmd.Name())
	}
	if _, _, err := rootCmd.Find([]string{"monitoring", "down"}); err != nil {
		t.Fatalf("monitoring down not registered: %v", err)
	}
}
