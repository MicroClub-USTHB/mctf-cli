package cmd

import (
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
)

func TestInstanceSpawnPayload(t *testing.T) {
	cfg := &config.Config{
		Event: config.Event{Slug: "test-event"},
	}
	expires := time.Date(2026, 10, 12, 16, 0, 0, 0, time.UTC)
	inst := instancer.Instance{
		ID:            "cub3-team9-xyz",
		ChallengeSlug: "web/cub3-clone",
		TeamID:        "team-9",
		ProjectName:   "cub3-team9-xyz",
		Containers:    []string{"cont-123", "cont-456"},
		Ports:         map[int]int{8000: 10042},
		Flag:          "mctf{dynamic_flag_test}",
		ExpiresAt:     expires,
	}

	payload := instanceSpawnPayload(cfg, inst)
	if payload.Event != "test-event" {
		t.Errorf("Event = %q, want test-event", payload.Event)
	}
	if payload.InstanceID != "cub3-team9-xyz" {
		t.Errorf("InstanceID = %q, want cub3-team9-xyz", payload.InstanceID)
	}
	if payload.ChallengeSlug != "web/cub3-clone" {
		t.Errorf("ChallengeSlug = %q, want web/cub3-clone", payload.ChallengeSlug)
	}
	if payload.TeamID != "team-9" {
		t.Errorf("TeamID = %q, want team-9", payload.TeamID)
	}
	if payload.Ports[8000] != 10042 {
		t.Errorf("Ports = %+v, want 8000 -> 10042", payload.Ports)
	}
	if payload.Flag != "mctf{dynamic_flag_test}" {
		t.Errorf("Flag = %q, want mctf{dynamic_flag_test}", payload.Flag)
	}
	if len(payload.Containers) != 2 || payload.Containers[0] != "cont-123" {
		t.Errorf("Containers = %+v", payload.Containers)
	}
	if !payload.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", payload.ExpiresAt, expires)
	}
}

func TestInstanceSpawnPayload_NilConfigSafe(t *testing.T) {
	inst := instancer.Instance{ID: "inst-1"}
	payload := instanceSpawnPayload(nil, inst)
	if payload.Event != "" {
		t.Errorf("Event = %q, want empty", payload.Event)
	}
	if payload.InstanceID != "inst-1" {
		t.Errorf("InstanceID = %q, want inst-1", payload.InstanceID)
	}
}
