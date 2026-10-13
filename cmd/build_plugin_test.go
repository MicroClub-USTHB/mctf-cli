package cmd

import (
	"errors"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/build"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func TestBuildHookPayload(t *testing.T) {
	cfg := &config.Config{
		Event: config.Event{Slug: "test-event"},
	}
	opts := build.Options{
		SHA:     "1a2b3c4",
		NoCache: true,
		Push:    false,
	}
	plan := []build.ImageSpec{
		{
			Challenge: "web/cub3-clone",
			Category:  "web",
			Slug:      "cub3-clone",
			Tags:      []string{"ghcr.io/test/web-cub3:1a2b3c4", "ghcr.io/test/web-cub3:latest"},
			Labels:    map[string]string{"mctf.challenge": "web/cub3-clone"},
		},
		{
			Challenge: "pwn/baby-overflow",
			Category:  "pwn",
			Slug:      "baby-overflow",
			Tags:      []string{"ghcr.io/test/pwn-baby:1a2b3c4"},
		},
	}

	// 1. "before" phase payload
	pBefore := buildHookPayload(cfg, opts, "before", true, plan, "", nil)
	if pBefore.Event != "test-event" {
		t.Errorf("Event = %q, want test-event", pBefore.Event)
	}
	if pBefore.Phase != "before" {
		t.Errorf("Phase = %q, want before", pBefore.Phase)
	}
	if !pBefore.DryRun {
		t.Errorf("DryRun = false, want true")
	}
	if !pBefore.NoCache {
		t.Errorf("NoCache = false, want true")
	}
	if pBefore.SHA != "1a2b3c4" {
		t.Errorf("SHA = %q, want 1a2b3c4", pBefore.SHA)
	}
	if len(pBefore.Images) != 2 {
		t.Fatalf("Images count = %d, want 2", len(pBefore.Images))
	}
	if pBefore.Images[0].Challenge != "web/cub3-clone" || pBefore.Images[0].Slug != "cub3-clone" {
		t.Errorf("Images[0] = %+v", pBefore.Images[0])
	}
	if pBefore.Images[0].Labels["mctf.challenge"] != "web/cub3-clone" {
		t.Errorf("Images[0].Labels = %+v", pBefore.Images[0].Labels)
	}
	if pBefore.Outcome != "" || pBefore.Error != "" {
		t.Errorf("unexpected outcome/error on before hook: %q / %q", pBefore.Outcome, pBefore.Error)
	}

	// 2. "after" success phase payload
	pAfterSuccess := buildHookPayload(cfg, opts, "after", false, plan, "success", nil)
	if pAfterSuccess.Phase != "after" {
		t.Errorf("Phase = %q, want after", pAfterSuccess.Phase)
	}
	if pAfterSuccess.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", pAfterSuccess.Outcome)
	}
	if pAfterSuccess.Error != "" {
		t.Errorf("Error = %q, want empty", pAfterSuccess.Error)
	}

	// 3. "after" failure phase payload
	errFailed := errors.New("docker daemon unresponsive")
	pAfterFail := buildHookPayload(cfg, opts, "after", false, plan, "failed", errFailed)
	if pAfterFail.Outcome != "failed" {
		t.Errorf("Outcome = %q, want failed", pAfterFail.Outcome)
	}
	if pAfterFail.Error != "docker daemon unresponsive" {
		t.Errorf("Error = %q, want docker daemon unresponsive", pAfterFail.Error)
	}
}

func TestBuildHookPayload_NilConfigSafe(t *testing.T) {
	p := buildHookPayload(nil, build.Options{}, "before", false, nil, "", nil)
	if p.Event != "" {
		t.Errorf("Event = %q, want empty", p.Event)
	}
	if len(p.Images) != 0 {
		t.Errorf("Images = %+v, want empty", p.Images)
	}
}
