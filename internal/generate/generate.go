package generate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/ingress"
)

// Outputs collects every file `mctf generate` would write, keyed by
// absolute path. Lets callers either write all files or compare against
// what's already on disk for --check mode.
type Outputs struct {
	Files map[string][]byte
}

// Plan computes every derived file given the loaded config and discovered
// challenges. Pure — does not touch the filesystem (except for reading
// existing source files via the loaders that fed challs).
func Plan(cfg *config.Config, eventRoot string, challs []*challenge.Challenge) (*Outputs, error) {
	out := &Outputs{Files: map[string][]byte{}}
	challengesRoot := absChallengesRoot(eventRoot, cfg)

	for _, c := range challs {
		// 1. CTFd manifest (every archetype)
		man, err := CTFdManifest(cfg, c)
		if err != nil {
			return nil, fmt.Errorf("ctfd manifest %s: %w", c.Slug, err)
		}
		out.Files[filepath.Join(c.RootDir, "challenge.yml")] = man

		// 2. instance.toml — only for instanced challenges
		toml, err := InstanceTOML(cfg, c)
		if err != nil {
			return nil, fmt.Errorf("instance.toml %s: %w", c.Slug, err)
		}
		if toml != nil {
			out.Files[filepath.Join(c.RootDir, "instance.toml")] = toml
		}
	}

	// 3. event-level CSV
	csv, err := CSV(challs)
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	out.Files[filepath.Join(challengesRoot, "challenges.csv")] = csv

	// 4. event-level JSON index
	idx, err := IndexJSON(cfg, challs)
	if err != nil {
		return nil, fmt.Errorf("index.json: %w", err)
	}
	out.Files[filepath.Join(challengesRoot, "index.json")] = idx

	// 5. Traefik ingress config (Docker mode + ingress=traefik only)
	if shouldEmitTraefik(cfg) {
		entries, err := ingress.PlanEntries(cfg, challs)
		if err != nil {
			return nil, fmt.Errorf("ingress: %w", err)
		}
		ingressDir := filepath.Join(eventRoot, "ingress", "traefik")
		out.Files[filepath.Join(ingressDir, "traefik.yml")] = ingress.TraefikStatic(cfg)
		out.Files[filepath.Join(ingressDir, "dynamic", "dynamic.yml")] = ingress.TraefikDynamic(entries)
	}

	prom, err := PrometheusConfig(cfg, challs)
	if err != nil {
		return nil, fmt.Errorf("prometheus: %w", err)
	}

	out.Files[filepath.Join(eventRoot, "monitoring", "prometheus.yml")] = prom

	return out, nil
}

func shouldEmitTraefik(cfg *config.Config) bool {
	if cfg == nil || cfg.Deploy == nil {
		return false
	}
	if cfg.Deploy.Mode != "docker" {
		return false
	}
	if cfg.Deploy.Docker == nil {
		return false
	}
	return cfg.Deploy.Docker.Ingress == "traefik"
}

// Apply writes every planned file. Files that already match on disk are
// skipped (so `git status` stays quiet when nothing changed).
//
// Also removes stale instance.toml from challenges that are no longer
// instanced — staleness is the silent-bug source we're guarding against.
func (o *Outputs) Apply() (written []string, removed []string, err error) {
	for path, data := range o.Files {
		if existing, e := os.ReadFile(path); e == nil && bytes.Equal(existing, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, removed, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return written, removed, err
		}
		written = append(written, path)
	}
	return written, removed, nil
}

// CleanStale removes derived files that exist on disk but are not in the
// plan (e.g. an instance.toml left over after a challenge stopped being
// instanced, or a derived challenge.yml whose source was deleted).
func (o *Outputs) CleanStale(cfg *config.Config, eventRoot string, challs []*challenge.Challenge) ([]string, error) {
	keep := map[string]bool{}
	for p := range o.Files {
		keep[p] = true
	}
	var removed []string
	for _, c := range challs {
		for _, name := range []string{"challenge.yml", "instance.toml"} {
			p := filepath.Join(c.RootDir, name)
			if keep[p] {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				if err := os.Remove(p); err != nil {
					return removed, err
				}
				removed = append(removed, p)
			}
		}
	}
	return removed, nil
}

// Diff compares planned outputs against what's on disk. Returns the list
// of files that would change; nil means everything is in sync.
func (o *Outputs) Diff() ([]Drift, error) {
	var drifts []Drift
	for path, want := range o.Files {
		got, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				drifts = append(drifts, Drift{Path: path, Reason: "missing"})
				continue
			}
			return nil, err
		}
		if !bytes.Equal(got, want) {
			drifts = append(drifts, Drift{Path: path, Reason: "stale"})
		}
	}
	return drifts, nil
}

// Drift describes one out-of-sync derived file.
type Drift struct {
	Path   string
	Reason string // "missing" | "stale"
}

func absChallengesRoot(eventRoot string, cfg *config.Config) string {
	dir := "./challenges"
	if cfg != nil && cfg.ChallengesDir != "" {
		dir = cfg.ChallengesDir
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(eventRoot, dir)
}
