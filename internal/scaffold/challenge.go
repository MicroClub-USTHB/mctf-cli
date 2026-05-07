package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ChallengeVars are the substitutions applied to the rendered challenge
// scaffold (manifest fields, compose top-level name, writeup title).
type ChallengeVars struct {
	Slug       string
	Category   string
	Name       string // display name; defaults to Slug if empty
	Author     string
	Difficulty string // optional override; "easy" if empty

	// Archetype controls the deploy block in the rendered manifest:
	// "static" (default) leaves it commented; "service" writes a
	// fixed-port deploy block; "instanced" writes a per-team block.
	Archetype string
}

// ChallengeOptions configures Challenge.
type ChallengeOptions struct {
	// EventRoot is the event repo root (absolute or relative to cwd).
	EventRoot string
	// ChallengesDir is the challenges directory relative to EventRoot,
	// e.g. "./challenges".
	ChallengesDir string
	// Force allows overwriting an existing challenge directory.
	Force bool
	// SourceDir, if non-empty, is used instead of the auto-detected
	// template (event-local or embedded). Mostly for tests.
	SourceDir string
}

// ErrChallengeExists is returned when the target challenge directory
// already exists and Force is false.
var ErrChallengeExists = errors.New("challenge already exists — pass --force to overwrite")

// Challenge scaffolds a new per-challenge directory tree under
// <EventRoot>/<ChallengesDir>/<Category>/<Slug>/, copying from the
// event-local templates/challenge_name/ if present, otherwise from the
// embedded fallback. The well-known fields in challenge.yml,
// docker-compose.yml, and writeup.md are rewritten to match vars.
func Challenge(vars ChallengeVars, opts ChallengeOptions) (*Result, error) {
	if vars.Slug == "" {
		return nil, fmt.Errorf("slug required")
	}
	if vars.Category == "" {
		return nil, fmt.Errorf("category required")
	}
	if vars.Name == "" {
		vars.Name = vars.Slug
	}
	if vars.Difficulty == "" {
		vars.Difficulty = "easy"
	}

	target := filepath.Join(opts.EventRoot, opts.ChallengesDir, vars.Category, vars.Slug)
	if _, err := os.Stat(target); err == nil && !opts.Force {
		return nil, fmt.Errorf("%w: %s", ErrChallengeExists, target)
	}

	src, useEmbed, err := resolveChallengeSource(opts)
	if err != nil {
		return nil, err
	}

	res := &Result{}
	if useEmbed {
		err = walkEmbedTo(src, target, vars, res, opts.Force)
	} else {
		err = walkDiskTo(src, target, vars, res, opts.Force)
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

// resolveChallengeSource returns the template source. If
// opts.SourceDir is set, that is used. Otherwise an event-local
// templates/challenge_name/ is preferred; embedded is the fallback.
func resolveChallengeSource(opts ChallengeOptions) (path string, useEmbed bool, err error) {
	if opts.SourceDir != "" {
		if _, err := os.Stat(opts.SourceDir); err != nil {
			return "", false, fmt.Errorf("source dir %s: %w", opts.SourceDir, err)
		}
		return opts.SourceDir, false, nil
	}
	local := filepath.Join(opts.EventRoot, "templates", "challenge_name")
	if info, err := os.Stat(local); err == nil && info.IsDir() {
		return local, false, nil
	}
	return "templates/event/templates/challenge_name", true, nil
}

func walkEmbedTo(src, target string, vars ChallengeVars, res *Result, force bool) error {
	return fs.WalkDir(eventFS, src, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == src {
			return os.MkdirAll(target, 0o755)
		}
		rel, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(eventFS, srcPath)
		if err != nil {
			return err
		}
		return writeChallengeFile(dst, rel, data, vars, res, force)
	})
}

func walkDiskTo(src, target string, vars ChallengeVars, res *Result, force bool) error {
	return filepath.WalkDir(src, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == src {
			return os.MkdirAll(target, 0o755)
		}
		rel, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		return writeChallengeFile(dst, rel, data, vars, res, force)
	})
}

func writeChallengeFile(dst, rel string, data []byte, vars ChallengeVars, res *Result, force bool) error {
	if _, err := os.Stat(dst); err == nil && !force {
		res.Skipped = append(res.Skipped, dst)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	switch rel {
	case filepath.Join("challenge", "challenge.yml"):
		data = []byte(rewriteManifest(string(data), vars))
	case filepath.Join("challenge", "docker-compose.yml"):
		data = []byte(rewriteCompose(string(data), vars))
	case filepath.Join("solution", "writeup.md"):
		data = []byte(rewriteWriteup(string(data), vars))
	}

	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	res.Written = append(res.Written, dst)
	return nil
}

// Rewrites are line-oriented and conservative: they touch only the
// well-known fields and preserve comments and formatting elsewhere.

var (
	reManifestName       = regexp.MustCompile(`(?m)^(\s*name:\s*)(?:".*?"|\S+)(.*)$`)
	reManifestAuthor     = regexp.MustCompile(`(?m)^(\s*author:\s*)(?:".*?"|\S+)(.*)$`)
	reManifestCategory   = regexp.MustCompile(`(?m)^(\s*category:\s*)\S+(.*)$`)
	reManifestDifficulty = regexp.MustCompile(`(?m)^(\s*difficulty:\s*)\S+(.*)$`)
	reNeedDeployFalse    = regexp.MustCompile(`(?m)^(\s*need_deploy:\s+)false`)
	reComposeName        = regexp.MustCompile(`(?m)^(name:\s*)\S+(.*)$`)
	reWriteupTitle       = regexp.MustCompile(`(?m)^# .*$`)
)

func rewriteManifest(body string, v ChallengeVars) string {
	body = reManifestName.ReplaceAllString(body, fmt.Sprintf(`${1}"%s"${2}`, escapeYAMLDouble(v.Name)))
	body = reManifestCategory.ReplaceAllString(body, fmt.Sprintf(`${1}%s${2}`, v.Category))
	body = reManifestDifficulty.ReplaceAllString(body, fmt.Sprintf(`${1}%s${2}`, v.Difficulty))
	if v.Author != "" {
		body = reManifestAuthor.ReplaceAllString(body, fmt.Sprintf(`${1}"%s"${2}`, escapeYAMLDouble(v.Author)))
	}
	if v.Archetype == "service" || v.Archetype == "instanced" {
		body = enableDeployBlock(body, v)
	}
	return body
}

// enableDeployBlock flips need_deploy to true and replaces the
// commented-out deploy block at the bottom of the template with a
// real one shaped for the requested archetype.
func enableDeployBlock(body string, v ChallengeVars) string {
	body = reNeedDeployFalse.ReplaceAllString(body, "${1}true")

	// Strip everything from the "# Uncomment when need_deploy: true."
	// marker through end of file (the commented-out template block).
	if idx := strings.Index(body, "# Uncomment when need_deploy: true."); idx >= 0 {
		body = strings.TrimRight(body[:idx], "\n") + "\n"
	}

	body += "\n" + deployBlock(v) + "\n"
	return body
}

func deployBlock(v ChallengeVars) string {
	subdomain := v.Slug
	switch v.Archetype {
	case "instanced":
		return strings.Join([]string{
			"deploy:",
			fmt.Sprintf("  subdomain: %q", subdomain),
			"  port: null",
			"  internal_port: 8000",
			"  dynamic_instances: true",
			"  tcp: false",
			"  noSsl: false",
			"  resources:",
			"    cpu: \"0.5\"",
			"    memory: \"256M\"",
			"  healthcheck:",
			"    type: http",
			"    path: \"/\"",
			"    interval: \"30s\"",
			"    timeout: \"5s\"",
			"    retries: 3",
		}, "\n")
	default: // service
		port := defaultServicePort(v.Category)
		return strings.Join([]string{
			"deploy:",
			fmt.Sprintf("  subdomain: %q", subdomain),
			fmt.Sprintf("  port: %d", port),
			"  internal_port: 8000",
			"  dynamic_instances: false",
			"  tcp: false",
			"  noSsl: false",
			"  resources:",
			"    cpu: \"0.5\"",
			"    memory: \"256M\"",
			"  healthcheck:",
			"    type: http",
			"    path: \"/\"",
			"    interval: \"30s\"",
			"    timeout: \"5s\"",
			"    retries: 3",
		}, "\n")
	}
}

// defaultServicePort returns a reasonable starting port within the
// category's port range. Authors should adjust if they conflict.
func defaultServicePort(category string) int {
	switch category {
	case "web":
		return 10500
	case "pwn":
		return 11500
	case "crypto":
		return 12500
	case "ai":
		return 13500
	case "misc":
		return 14500
	case "reverse":
		return 15500
	case "jailbreak":
		return 16500
	default:
		return 14000
	}
}

func rewriteCompose(body string, v ChallengeVars) string {
	// Compose's top-level "name:" wants a DNS-label slug, not the display name.
	return reComposeName.ReplaceAllString(body, fmt.Sprintf(`${1}%s${2}`, v.Slug))
}

func rewriteWriteup(body string, v ChallengeVars) string {
	// Replace the first level-1 heading.
	replaced := false
	return reWriteupTitle.ReplaceAllStringFunc(body, func(match string) string {
		if replaced {
			return match
		}
		replaced = true
		return fmt.Sprintf("# %s — writeup", v.Name)
	})
}

func escapeYAMLDouble(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
}
