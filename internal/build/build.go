// Package build plans and executes container image builds for the
// deployable challenges in an event repo.
//
// Plan() is a pure function that returns ImageSpec entries for every
// challenge that requires a build. Apply() walks the plan with a
// caller-supplied Runner; the Docker implementation lives next door in
// runner.go but tests can substitute a fake.
package build

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// ImageSpec is what the builder needs to produce one image.
type ImageSpec struct {
	Challenge  string            // <category>/<slug>
	Category   string
	Slug       string
	Context    string            // absolute path to challenge/ folder
	Dockerfile string            // absolute path
	Tags       []string          // primary then :latest
	Labels     map[string]string // OCI labels stamped on the image
	// FlagValues are the manifest-declared flag values for this
	// challenge — used by the post-build leak scan. Empty for static
	// challenges (which never produce an image anyway).
	FlagValues []string
}

// Options narrows or alters the plan.
type Options struct {
	// CategoryFilter, if set, limits the plan to challenges whose
	// category equals this value.
	CategoryFilter string
	// SlugFilter, if set, limits the plan to a single challenge slug.
	SlugFilter string
	// SHA is stamped into the primary image tag. Callers usually
	// supply git rev-parse --short HEAD; if empty, "dev" is used.
	SHA string
	// NoCache passes --no-cache to docker.
	NoCache bool
	// Push pushes both tags after a successful build.
	Push bool
}

// Plan walks challs and produces an ImageSpec for each deployable one
// after applying filters. The result is sorted by Challenge for stable
// output.
func Plan(cfg *config.Config, eventRoot string, challs []*challenge.Challenge, opts Options) ([]ImageSpec, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	registry := ""
	if cfg.Registry != nil {
		registry = strings.TrimRight(cfg.Registry.URL, "/")
	}
	if registry == "" {
		return nil, fmt.Errorf("registry.url not set in mtcf.config.yml — required for build")
	}
	sha := opts.SHA
	if sha == "" {
		sha = "dev"
	}

	var out []ImageSpec
	for _, c := range challs {
		if !c.Manifest.Challenge.NeedDeploy {
			continue
		}
		if opts.CategoryFilter != "" && c.CategoryDir != opts.CategoryFilter {
			continue
		}
		if opts.SlugFilter != "" && c.Slug != opts.SlugFilter {
			continue
		}
		if !c.HasDockerfile() {
			return nil, fmt.Errorf("%s/%s: need_deploy=true but challenge/Dockerfile is missing", c.CategoryDir, c.Slug)
		}
		base := fmt.Sprintf("%s/%s-%s-%s", registry, cfg.Event.Slug, c.CategoryDir, c.Slug)
		ctxPath, _ := filepath.Abs(c.ChallengeDir)
		flagVals := make([]string, 0, len(c.Manifest.Challenge.Flags))
		for _, f := range c.Manifest.Challenge.Flags {
			if f.Flag != "" {
				flagVals = append(flagVals, f.Flag)
			}
		}
		out = append(out, ImageSpec{
			Challenge:  c.CategoryDir + "/" + c.Slug,
			Category:   c.CategoryDir,
			Slug:       c.Slug,
			Context:    ctxPath,
			Dockerfile: filepath.Join(ctxPath, "Dockerfile"),
			Tags:       []string{base + ":" + sha, base + ":latest"},
			Labels: map[string]string{
				"mtcf.event":     cfg.Event.Slug,
				"mtcf.category":  c.CategoryDir,
				"mtcf.challenge": c.Slug,
				"mtcf.archetype": string(c.Archetype),
			},
			FlagValues: flagVals,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Challenge < out[j].Challenge })
	return out, nil
}

// Runner executes one build. Returns the command stdout/stderr only on
// failure; success returns nil with no output (callers stream output
// directly through stdout/stderr).
type Runner interface {
	Build(ctx context.Context, spec ImageSpec, opts Options) error
	Push(ctx context.Context, spec ImageSpec, opts Options) error
}

// Apply runs the plan in order with the given Runner. Stops on the first
// error and returns it; the caller decides how to surface partial state.
func Apply(ctx context.Context, r Runner, plan []ImageSpec, opts Options) error {
	for _, spec := range plan {
		if err := r.Build(ctx, spec, opts); err != nil {
			return fmt.Errorf("%s: build failed: %w", spec.Challenge, err)
		}
		if opts.Push {
			if err := r.Push(ctx, spec, opts); err != nil {
				return fmt.Errorf("%s: push failed: %w", spec.Challenge, err)
			}
		}
	}
	return nil
}

// BuildArgs renders the docker CLI args for one ImageSpec. Pure
// function — no exec — so tests can pin the command shape.
func BuildArgs(spec ImageSpec, opts Options) []string {
	args := []string{"build"}
	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	for _, tag := range spec.Tags {
		args = append(args, "-t", tag)
	}
	keys := make([]string, 0, len(spec.Labels))
	for k := range spec.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, spec.Labels[k]))
	}
	args = append(args, "-f", spec.Dockerfile, spec.Context)
	return args
}

// PushArgs renders the docker CLI args for one push (one per tag).
func PushArgs(tag string) []string {
	return []string{"push", tag}
}

// GitSHA returns the short HEAD sha of the repo at dir, or "" if dir
// isn't a git repo or git isn't installed.
func GitSHA(dir string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
