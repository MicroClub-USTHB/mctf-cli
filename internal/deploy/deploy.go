// Package deploy plans and executes per-challenge `docker compose` runs.
//
// v0 scope: Docker mode only, service archetype only. Instanced
// challenges are intentionally skipped here — they are spawned per-team
// at game time by the instancer, not at the event-deploy step.
package deploy

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Unit is one compose project corresponding to one challenge.
type Unit struct {
	Challenge   string // <category>/<slug>
	Category    string
	Slug        string
	ProjectName string            // mctf-<event>-<cat>-<slug>
	Cwd         string            // working dir for `docker compose` (the challenge/ folder)
	ComposeFile string            // absolute path to docker-compose.yml
	Env         map[string]string // env passed to docker compose (PORT_<n>, etc.)
	ImageTag    string            // sha tag used for image interpolation
}

// Options narrow or alter the plan.
type Options struct {
	CategoryFilter   string
	SlugFilter       string
	IncludeInstanced bool   // override v0 default (skip instanced)
	ImageTag         string // optional: sha used to stamp images
}

// Plan returns one Unit per deployable, service-archetype challenge
// (after filters), in stable order.
func Plan(cfg *config.Config, eventRoot string, challs []*challenge.Challenge, opts Options) ([]Unit, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	if cfg.Deploy == nil || cfg.Deploy.Mode != "docker" && cfg.Deploy.Mode != "ssh" {
		return nil, fmt.Errorf("deploy.mode must be 'docker' or 'ssh' (got %q) — other modes ship in later releases",
			deployMode(cfg))
	}

	var out []Unit
	for _, c := range challs {
		if !c.Manifest.Challenge.NeedDeploy {
			continue
		}
		if c.Archetype == challenge.ArchetypeInstanced && !opts.IncludeInstanced {
			continue
		}
		if opts.CategoryFilter != "" && c.CategoryDir != opts.CategoryFilter {
			continue
		}
		if opts.SlugFilter != "" && c.Slug != opts.SlugFilter {
			continue
		}
		if !c.HasCompose() {
			return nil, fmt.Errorf("%s/%s: need_deploy=true but challenge/docker-compose.yml is missing", c.CategoryDir, c.Slug)
		}
		cwd, _ := filepath.Abs(c.ChallengeDir)
		u := Unit{
			Challenge:   c.CategoryDir + "/" + c.Slug,
			Category:    c.CategoryDir,
			Slug:        c.Slug,
			ProjectName: ProjectName(cfg, c),
			Cwd:         cwd,
			ComposeFile: filepath.Join(cwd, "docker-compose.yml"),
			Env:         envForUnit(c),
			ImageTag:    opts.ImageTag,
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Challenge < out[j].Challenge })
	return out, nil
}

// ProjectName builds the docker compose project name for one challenge.
//
// Format: mctf-<event-slug>-<category>-<slug>. The compose project name
// is the visible identity for `docker compose ls`, so making it stable
// and self-describing matters.
func ProjectName(cfg *config.Config, c *challenge.Challenge) string {
	return fmt.Sprintf("mctf-%s-%s-%s", cfg.Event.Slug, c.CategoryDir, c.Slug)
}

// envForUnit derives the env vars the compose run needs. For service
// challenges with internal_port set, it stamps PORT_<internal> with
// the manifest's port so the compose's ${PORT_N:-N} convention picks
// the right host port.
//
// The pinned image tag is written literally into docker-compose.mctf.yml
// by TranslateCompose, so it is not passed through the environment.
func envForUnit(c *challenge.Challenge) map[string]string {
	if c.Manifest.Deploy == nil {
		return nil
	}
	d := c.Manifest.Deploy
	if d.InternalPort != nil && d.Port != nil {
		return map[string]string{
			fmt.Sprintf("PORT_%d", *d.InternalPort): fmt.Sprintf("%d", *d.Port),
		}
	}
	return nil
}

// UpArgs renders the docker CLI args for `docker compose up -d`.
// Pure function so tests can pin the command shape.
func UpArgs(u Unit) []string {
	return []string{"compose", "-p", u.ProjectName, "-f", u.ComposeFile, "up", "-d"}
}

// DownArgs renders the docker CLI args for `docker compose down`.
// volumes=true also strips named volumes (-v).
func DownArgs(u Unit, volumes bool) []string {
	args := []string{"compose", "-p", u.ProjectName, "-f", u.ComposeFile, "down"}
	if volumes {
		args = append(args, "-v")
	}
	return args
}

// Runner executes one Unit. Implementations are in runner.go.
type Runner interface {
	Up(ctx context.Context, u Unit) error
	Down(ctx context.Context, u Unit, volumes bool) error
}

// ApplyUp calls Up on each Unit in order.
func ApplyUp(ctx context.Context, r Runner, plan []Unit) error {
	for _, u := range plan {
		if err := r.Up(ctx, u); err != nil {
			return fmt.Errorf("%s: up failed: %w", u.Challenge, err)
		}
	}
	return nil
}

// ApplyDown calls Down on each Unit in reverse order (last-up-first-down).
func ApplyDown(ctx context.Context, r Runner, plan []Unit, volumes bool) error {
	for i := len(plan) - 1; i >= 0; i-- {
		u := plan[i]
		if err := r.Down(ctx, u, volumes); err != nil {
			return fmt.Errorf("%s: down failed: %w", u.Challenge, err)
		}
	}
	return nil
}

func deployMode(cfg *config.Config) string {
	if cfg.Deploy == nil {
		return ""
	}
	return cfg.Deploy.Mode
}

// EnvSlice returns env as KEY=VALUE pairs, sorted by key, suitable for
// passing to exec.Cmd.Env (after appending os.Environ()).
func EnvSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// CommandPreview renders a human-readable command-line preview for one
// docker compose invocation. Used by --dry-run output and tests.
func CommandPreview(args []string, env map[string]string) string {
	var sb strings.Builder
	for _, kv := range EnvSlice(env) {
		sb.WriteString(kv)
		sb.WriteString(" ")
	}
	sb.WriteString("docker")
	for _, a := range args {
		sb.WriteString(" ")
		sb.WriteString(a)
	}
	return sb.String()
}
