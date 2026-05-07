package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func cfgDocker() *config.Config {
	return &config.Config{
		Version: 1,
		Event:   config.Event{Slug: "evt"},
		Deploy:  &config.Deploy{Mode: "docker"},
	}
}

func newChall(t *testing.T, root, cat, slug string, arch challenge.Archetype, withCompose bool, port, internal int) *challenge.Challenge {
	t.Helper()
	chRoot := filepath.Join(root, cat, slug)
	chDir := filepath.Join(chRoot, "challenge")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if withCompose {
		_ = os.WriteFile(filepath.Join(chDir, "docker-compose.yml"), []byte("name: x\n"), 0o644)
	}
	c := &challenge.Challenge{
		Slug: slug, CategoryDir: cat, RootDir: chRoot, ChallengeDir: chDir,
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: slug, Category: cat, NeedDeploy: arch != challenge.ArchetypeStatic,
			},
		},
		Archetype: arch,
	}
	if arch != challenge.ArchetypeStatic {
		d := &challenge.DeployBlock{}
		if port > 0 {
			d.Port = &port
		}
		if internal > 0 {
			d.InternalPort = &internal
		}
		d.DynamicInstances = arch == challenge.ArchetypeInstanced
		c.Manifest.Deploy = d
	}
	return c
}

func TestPlan_DockerOnly(t *testing.T) {
	cfg := &config.Config{Version: 1, Event: config.Event{Slug: "evt"}, Deploy: &config.Deploy{Mode: "ssh"}}
	_, err := Plan(cfg, "/tmp", nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "deploy.mode must be 'docker'") {
		t.Errorf("expected docker-mode error, got %v", err)
	}
}

func TestPlan_SkipsStaticAndInstanced(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		newChall(t, root, "crypto", "static-one", challenge.ArchetypeStatic, false, 0, 0),
		newChall(t, root, "misc", "service-one", challenge.ArchetypeService, true, 14000, 8000),
		newChall(t, root, "web", "instanced-one", challenge.ArchetypeInstanced, true, 0, 8000),
	}
	plan, err := Plan(cfgDocker(), root, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 || plan[0].Challenge != "misc/service-one" {
		t.Errorf("plan should have only the service challenge, got %+v", plan)
	}
}

func TestPlan_IncludeInstancedFlag(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		newChall(t, root, "web", "i", challenge.ArchetypeInstanced, true, 0, 8000),
	}
	plan, err := Plan(cfgDocker(), root, challs, Options{IncludeInstanced: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 {
		t.Errorf("with IncludeInstanced, expected 1 entry, got %d", len(plan))
	}
}

func TestPlan_ProjectName(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		newChall(t, root, "misc", "welcome", challenge.ArchetypeService, true, 14000, 8000),
	}
	plan, err := Plan(cfgDocker(), root, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].ProjectName != "mtcf-evt-misc-welcome" {
		t.Errorf("project name = %q", plan[0].ProjectName)
	}
}

func TestPlan_StampsPortEnv(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		newChall(t, root, "misc", "welcome", challenge.ArchetypeService, true, 14000, 8000),
	}
	plan, err := Plan(cfgDocker(), root, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan[0].Env["PORT_8000"]; got != "14000" {
		t.Errorf("PORT_8000 = %q (expected 14000)", got)
	}
}

func TestPlan_NoEnvWhenInternalUnset(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		newChall(t, root, "misc", "x", challenge.ArchetypeService, true, 14000, 0),
	}
	plan, err := Plan(cfgDocker(), root, challs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan[0].Env) != 0 {
		t.Errorf("expected no env when internal_port unset, got %+v", plan[0].Env)
	}
}

func TestPlan_MissingComposeErrors(t *testing.T) {
	root := t.TempDir()
	challs := []*challenge.Challenge{
		newChall(t, root, "misc", "x", challenge.ArchetypeService, false, 14000, 8000),
	}
	_, err := Plan(cfgDocker(), root, challs, Options{})
	if err == nil || !strings.Contains(err.Error(), "docker-compose.yml") {
		t.Errorf("expected missing-compose error, got %v", err)
	}
}

func TestUpArgs_Shape(t *testing.T) {
	u := Unit{ProjectName: "mtcf-evt-misc-welcome", ComposeFile: "/abs/docker-compose.yml"}
	got := strings.Join(UpArgs(u), " ")
	want := "compose -p mtcf-evt-misc-welcome -f /abs/docker-compose.yml up -d"
	if got != want {
		t.Errorf("UpArgs = %q, want %q", got, want)
	}
}

func TestDownArgs_Volumes(t *testing.T) {
	u := Unit{ProjectName: "p", ComposeFile: "/abs/x.yml"}
	got := strings.Join(DownArgs(u, true), " ")
	if !strings.Contains(got, "down -v") {
		t.Errorf("DownArgs missing -v: %q", got)
	}
	got = strings.Join(DownArgs(u, false), " ")
	if strings.Contains(got, "-v") {
		t.Errorf("DownArgs unexpected -v: %q", got)
	}
}

func TestEnvSlice_Sorted(t *testing.T) {
	got := EnvSlice(map[string]string{"Z": "1", "A": "2", "M": "3"})
	want := []string{"A=2", "M=3", "Z=1"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("EnvSlice = %v, want %v", got, want)
	}
}

func TestCommandPreview_StampsEnv(t *testing.T) {
	u := Unit{ProjectName: "p", ComposeFile: "/x.yml", Env: map[string]string{"PORT_8000": "14000"}}
	got := CommandPreview(UpArgs(u), u.Env)
	if !strings.HasPrefix(got, "PORT_8000=14000 docker") {
		t.Errorf("preview = %q", got)
	}
}

type recRunner struct {
	upOrder, downOrder []string
	failOn             string
}

func (r *recRunner) Up(_ context.Context, u Unit) error {
	if u.Challenge == r.failOn {
		return errors.New("nope")
	}
	r.upOrder = append(r.upOrder, u.Challenge)
	return nil
}
func (r *recRunner) Down(_ context.Context, u Unit, _ bool) error {
	r.downOrder = append(r.downOrder, u.Challenge)
	return nil
}

func TestApplyDown_ReverseOrder(t *testing.T) {
	r := &recRunner{}
	plan := []Unit{
		{Challenge: "a"}, {Challenge: "b"}, {Challenge: "c"},
	}
	if err := ApplyDown(context.Background(), r, plan, false); err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "b", "a"}
	if fmt.Sprintf("%v", r.downOrder) != fmt.Sprintf("%v", want) {
		t.Errorf("down order = %v, want %v", r.downOrder, want)
	}
}

func TestApplyUp_StopsOnError(t *testing.T) {
	r := &recRunner{failOn: "b"}
	plan := []Unit{{Challenge: "a"}, {Challenge: "b"}, {Challenge: "c"}}
	err := ApplyUp(context.Background(), r, plan)
	if err == nil || !strings.Contains(err.Error(), "b") {
		t.Errorf("expected error mentioning b, got %v", err)
	}
	if len(r.upOrder) != 1 || r.upOrder[0] != "a" {
		t.Errorf("expected only 'a' to have run, got %v", r.upOrder)
	}
}
