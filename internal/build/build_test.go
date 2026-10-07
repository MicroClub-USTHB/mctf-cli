package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func sampleConfig() *config.Config {
	return &config.Config{
		Version:  1,
		Event:    config.Event{Slug: "evt"},
		Registry: &config.Registry{URL: "ghcr.io/example/"},
	}
}

func sampleChallenge(t *testing.T, root, cat, slug string, deploy, withDocker bool) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, cat, slug)
	chDir := filepath.Join(chRoot, "challenge")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if withDocker {
		if err := os.WriteFile(filepath.Join(chDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &challenge.Challenge{
		Slug:         slug,
		CategoryDir:  cat,
		RootDir:      chRoot,
		ChallengeDir: chDir,
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Category: cat, NeedDeploy: deploy,
			},
		},
		Archetype: func() challenge.Archetype {
			if !deploy {
				return challenge.ArchetypeStatic
			}
			return challenge.ArchetypeService
		}(),
	}
}

func TestPlan_OnlyDeployable(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		sampleChallenge(t, root, "web", "alpha", true, true),
		sampleChallenge(t, root, "crypto", "beta", false, false), // static — skipped
		sampleChallenge(t, root, "pwn", "gamma", true, true),
	}
	plan, err := Plan(sampleConfig(), root, challs, Options{SHA: "abc1234"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(plan), plan)
	}
	if plan[0].Challenge != "pwn/gamma" || plan[1].Challenge != "web/alpha" {
		t.Errorf("plan order wrong: %+v", plan)
	}
}

func TestPlan_TagFormat(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{sampleChallenge(t, root, "web", "cub3", true, true)}
	plan, err := Plan(sampleConfig(), root, challs, Options{SHA: "deadbee"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 {
		t.Fatal("expected 1 entry")
	}
	want := []string{
		"ghcr.io/example/evt-web-cub3:deadbee",
		"ghcr.io/example/evt-web-cub3:latest",
	}
	if plan[0].Tags[0] != want[0] || plan[0].Tags[1] != want[1] {
		t.Errorf("tags = %v, want %v", plan[0].Tags, want)
	}
}

func TestPlan_DefaultSHA(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{sampleChallenge(t, root, "web", "x", true, true)}
	plan, err := Plan(sampleConfig(), root, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(plan[0].Tags[0], ":dev") {
		t.Errorf("expected default :dev tag, got %s", plan[0].Tags[0])
	}
}

func TestPlan_FilterByCategory(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		sampleChallenge(t, root, "web", "a", true, true),
		sampleChallenge(t, root, "pwn", "b", true, true),
	}
	plan, err := Plan(sampleConfig(), root, challs, Options{CategoryFilter: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Challenge != "web/a" {
		t.Errorf("filter wrong: %+v", plan)
	}
}

func TestPlan_FilterBySlug(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		sampleChallenge(t, root, "web", "a", true, true),
		sampleChallenge(t, root, "web", "b", true, true),
	}
	plan, err := Plan(sampleConfig(), root, challs, Options{SlugFilter: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Slug != "b" {
		t.Errorf("filter wrong: %+v", plan)
	}
}

func TestPlan_MissingDockerfileErrors(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{sampleChallenge(t, root, "web", "x", true, false)}
	_, err := Plan(sampleConfig(), root, challs, Options{})
	if err == nil || !strings.Contains(err.Error(), "Dockerfile is missing") {
		t.Errorf("expected missing-Dockerfile error, got %v", err)
	}
}

func TestPlan_NoRegistryErrors(t *testing.T) {
	cfg := sampleConfig()
	cfg.Registry = nil
	_, err := Plan(cfg, "/tmp", nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "registry.url") {
		t.Errorf("expected registry error, got %v", err)
	}
}

func TestBuildArgs_Shape(t *testing.T) {
	spec := ImageSpec{
		Context:    "/repo/web/x/challenge",
		Dockerfile: "/repo/web/x/challenge/Dockerfile",
		Tags:       []string{"reg/x:abc", "reg/x:latest"},
		Labels:     map[string]string{"mctf.event": "evt", "mctf.category": "web"},
	}
	args := BuildArgs(spec, Options{NoCache: true})
	got := strings.Join(args, " ")
	for _, want := range []string{
		"build",
		"--no-cache",
		"-t reg/x:abc",
		"-t reg/x:latest",
		"--label mctf.category=web",
		"--label mctf.event=evt",
		"-f /repo/web/x/challenge/Dockerfile",
		"/repo/web/x/challenge",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("BuildArgs missing %q\nfull: %s", want, got)
		}
	}
}

func TestBuildArgs_LabelsSorted(t *testing.T) {
	// Labels must render in sorted order so the resulting command is
	// stable and reproducible.
	spec := ImageSpec{
		Tags:   []string{"x"},
		Labels: map[string]string{"z": "1", "a": "1", "m": "1"},
	}
	args := BuildArgs(spec, Options{})
	got := strings.Join(args, " ")
	first := strings.Index(got, "--label a=")
	mid := strings.Index(got, "--label m=")
	last := strings.Index(got, "--label z=")
	if first == -1 || mid == -1 || last == -1 {
		t.Fatalf("labels missing: %s", got)
	}
	if first >= mid || mid >= last {
		t.Errorf("labels not sorted: %s", got)
	}
}

type recordingRunner struct {
	builds, pushes []string
	failOn         string
}

func (r *recordingRunner) Build(_ context.Context, s ImageSpec, _ Options) error {
	if r.failOn == s.Challenge {
		return errors.New("boom")
	}
	r.builds = append(r.builds, s.Challenge)
	return nil
}
func (r *recordingRunner) Push(_ context.Context, s ImageSpec, _ Options) error {
	r.pushes = append(r.pushes, s.Challenge)
	return nil
}

func TestApply_StopsOnError(t *testing.T) {
	r := &recordingRunner{failOn: "web/b"}
	plan := []ImageSpec{
		{Challenge: "web/a", Tags: []string{"x"}},
		{Challenge: "web/b", Tags: []string{"y"}},
		{Challenge: "web/c", Tags: []string{"z"}},
	}
	err := Apply(context.Background(), r, plan, Options{})
	if err == nil || !strings.Contains(err.Error(), "web/b") {
		t.Errorf("want error mentioning web/b, got %v", err)
	}
	if len(r.builds) != 1 || r.builds[0] != "web/a" {
		t.Errorf("expected a build before failure, got %v", r.builds)
	}
}

func TestApply_PushesWhenAsked(t *testing.T) {
	r := &recordingRunner{}
	plan := []ImageSpec{{Challenge: "web/a", Tags: []string{"x"}}}
	if err := Apply(context.Background(), r, plan, Options{Push: true}); err != nil {
		t.Fatal(err)
	}
	if len(r.builds) != 1 || len(r.pushes) != 1 {
		t.Errorf("builds=%v pushes=%v", r.builds, r.pushes)
	}
}

func TestApply_BuildsAllBeforePush(t *testing.T) {
	r := &recordingRunner{failOn: "web/b"}
	plan := []ImageSpec{
		{Challenge: "web/a", Tags: []string{"x"}},
		{Challenge: "web/b", Tags: []string{"y"}},
	}
	_ = Apply(context.Background(), r, plan, Options{Push: true})
	if len(r.pushes) != 0 {
		t.Errorf("expected 0 pushes when a subsequent build fails, got %v", r.pushes)
	}
}
