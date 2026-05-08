package instancer

import (
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func instancedChall(slug, cat string, internalPort int) *challenge.Challenge {
	ip := internalPort
	return &challenge.Challenge{
		Slug:         slug,
		CategoryDir:  cat,
		ChallengeDir: "/event/challenges/" + cat + "/" + slug + "/challenge",
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Category: cat, NeedDeploy: true,
				Flags: []challenge.Flag{{Flag: "mctf{static-flag}", CaseSensitive: true}},
			},
			Deploy: &challenge.DeployBlock{
				DynamicInstances: true,
				InternalPort:     &ip,
				Subdomain:        slug,
			},
		},
		Archetype: challenge.ArchetypeInstanced,
	}
}

func cfgWithInstancer() *config.Config {
	return &config.Config{
		Version:   1,
		Event:     config.Event{Slug: "evt"},
		Instancer: &config.Instancer{DefaultTimeout: 600},
	}
}

func TestPlanSpawn_RejectsNonInstanced(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	c.Archetype = challenge.ArchetypeService
	_, err := PlanSpawn(cfgWithInstancer(), c, "team1", nil, SpawnOptions{})
	if err == nil || !strings.Contains(err.Error(), "not instanced") {
		t.Errorf("expected non-instanced error, got %v", err)
	}
}

func TestPlanSpawn_RequiresInternalPort(t *testing.T) {
	c := instancedChall("x", "web", 0)
	c.Manifest.Deploy.InternalPort = nil
	_, err := PlanSpawn(cfgWithInstancer(), c, "team1", nil, SpawnOptions{})
	if err == nil || !strings.Contains(err.Error(), "internal_port") {
		t.Errorf("expected internal_port error, got %v", err)
	}
}

func TestPlanSpawn_RequiresTeam(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	_, err := PlanSpawn(cfgWithInstancer(), c, "", nil, SpawnOptions{})
	if err == nil {
		t.Error("expected teamID-required error")
	}
}

func TestPlanSpawn_AssignsPortInRange(t *testing.T) {
	c := instancedChall("cub3", "web", 8000)
	spec, err := PlanSpawn(cfgWithInstancer(), c, "alpha", nil, SpawnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	host := spec.Ports[8000]
	if host < 30000 || host > 32999 {
		t.Errorf("host port %d out of default range 30000-32999", host)
	}
	if spec.Env["PORT_8000"] != "" {
		// stamped, not empty
	} else {
		t.Error("PORT_8000 env not stamped")
	}
}

func TestPlanSpawn_AvoidsAllocatedPorts(t *testing.T) {
	c := instancedChall("cub3", "web", 8000)
	// Pre-allocate everything except port 30050.
	allocated := map[int]bool{}
	for p := 30000; p <= 32999; p++ {
		if p != 30050 {
			allocated[p] = true
		}
	}
	spec, err := PlanSpawn(cfgWithInstancer(), c, "alpha", allocated, SpawnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Ports[8000] != 30050 {
		t.Errorf("expected port 30050 (only free), got %d", spec.Ports[8000])
	}
}

func TestPlanSpawn_NoFreePortsErrors(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	allocated := map[int]bool{}
	for p := 30000; p <= 32999; p++ {
		allocated[p] = true
	}
	_, err := PlanSpawn(cfgWithInstancer(), c, "team", allocated, SpawnOptions{})
	if err == nil || !strings.Contains(err.Error(), "no free port") {
		t.Errorf("expected port-exhaustion error, got %v", err)
	}
}

func TestPlanSpawn_StampsExpiry(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	frozen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	spec, err := PlanSpawn(cfgWithInstancer(), c, "team", nil,
		SpawnOptions{Now: func() time.Time { return frozen }})
	if err != nil {
		t.Fatal(err)
	}
	want := frozen.Add(600 * time.Second).UTC()
	if !spec.ExpiresAt.Equal(want) {
		t.Errorf("expires = %v, want %v", spec.ExpiresAt, want)
	}
}

func TestPlanSpawn_PerChallengeTimeoutWins(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	c.Manifest.Deploy.Instance = &challenge.InstanceCfg{Timeout: 60}
	frozen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	spec, _ := PlanSpawn(cfgWithInstancer(), c, "team", nil,
		SpawnOptions{Now: func() time.Time { return frozen }})
	if got := spec.ExpiresAt.Sub(frozen); got != 60*time.Second {
		t.Errorf("per-challenge timeout = %v, want 60s", got)
	}
}

func TestPlanSpawn_FlagOverride(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	spec, _ := PlanSpawn(cfgWithInstancer(), c, "team", nil,
		SpawnOptions{FlagOverride: "mctf{per-team}"})
	if spec.Env["FLAG"] != "mctf{per-team}" {
		t.Errorf("FLAG = %q, want mctf{per-team}", spec.Env["FLAG"])
	}
}

func TestPlanSpawn_FallsBackToManifestFlag(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	spec, _ := PlanSpawn(cfgWithInstancer(), c, "team", nil, SpawnOptions{})
	if spec.Env["FLAG"] != "mctf{static-flag}" {
		t.Errorf("FLAG = %q, want manifest static flag", spec.Env["FLAG"])
	}
}

func TestPlanSpawn_IDIsUniquePerCall(t *testing.T) {
	c := instancedChall("x", "web", 8000)
	a, _ := PlanSpawn(cfgWithInstancer(), c, "team", nil, SpawnOptions{})
	b, _ := PlanSpawn(cfgWithInstancer(), c, "team", nil, SpawnOptions{})
	if a.ID == b.ID {
		t.Errorf("two PlanSpawn calls produced the same ID: %s", a.ID)
	}
	for _, prefix := range []string{"mtcf-inst-x-team-"} {
		if !strings.HasPrefix(a.ID, prefix) {
			t.Errorf("ID %q lacks expected prefix %q", a.ID, prefix)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Hello World": "hello-world",
		"A_B/C":       "a-b-c",
		"---x---":     "x",
		"12_34":       "12-34",
		"":            "x",
		"mctf6.0":     "mctf60", // dots are dropped (version-style slugs)
		"x  y":        "x-y",    // collapsed dashes
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpArgs_AndDestroyArgs(t *testing.T) {
	s := SpawnSpec{ID: "p", ComposeFile: "/x.yml"}
	if got := strings.Join(UpArgs(s), " "); got != "compose -p p -f /x.yml up -d" {
		t.Errorf("UpArgs = %q", got)
	}
	if got := strings.Join(DestroyArgs(s, true), " "); got != "compose -p p -f /x.yml down -v" {
		t.Errorf("DestroyArgs(volumes) = %q", got)
	}
}

func TestEnvSlice_Sorted(t *testing.T) {
	s := SpawnSpec{Env: map[string]string{"Z": "1", "A": "2", "M": "3"}}
	got := s.EnvSlice()
	want := []string{"A=2", "M=3", "Z=1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("EnvSlice = %v, want %v", got, want)
	}
}
