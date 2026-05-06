package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// resolvedScoring is the effective scoring config for one challenge.
type resolvedScoring struct {
	Type    string // "static" | "dynamic"
	Initial int
	Decay   int
	Minimum int
	Points  int
}

// resolveScoring returns the effective scoring for a challenge:
// per-challenge override > event default > minimal static fallback.
func resolveScoring(cfg *config.Config, perChall *challenge.Scoring) resolvedScoring {
	r := resolvedScoring{Type: "dynamic", Initial: 500, Decay: 10, Minimum: 100}
	if cfg != nil && cfg.Event.Scoring != nil {
		r = resolvedScoring{
			Type:    cfg.Event.Scoring.Type,
			Initial: cfg.Event.Scoring.Initial,
			Decay:   cfg.Event.Scoring.Decay,
			Minimum: cfg.Event.Scoring.Minimum,
			Points:  cfg.Event.Scoring.Points,
		}
	}
	if perChall != nil {
		r = resolvedScoring{
			Type:    perChall.Type,
			Initial: perChall.Initial,
			Decay:   perChall.Decay,
			Minimum: perChall.Minimum,
			Points:  perChall.Points,
		}
	}
	if r.Type == "static" && r.Points == 0 {
		r.Points = 100
	}
	return r
}

// connectionInfo computes the URL/host:port shown to players.
//
// Static -> nil; instanced -> the instancer's public URL; service+http
// -> https://<subdomain>.<domain>; service+tcp -> <domain>:<port>.
func connectionInfo(cfg *config.Config, c *challenge.Challenge) *string {
	if challenge.DeriveArchetype(c.Manifest) == challenge.ArchetypeStatic {
		return nil
	}
	if c.Archetype == challenge.ArchetypeInstanced {
		if cfg != nil && cfg.Instancer != nil && cfg.Instancer.URL != "" {
			s := cfg.Instancer.URL
			return &s
		}
		s := ""
		return &s
	}
	// service archetype
	dep := c.Manifest.Deploy
	domain := ""
	if cfg != nil {
		domain = cfg.Event.Domain
	}
	if dep == nil {
		return nil
	}
	if dep.TCP {
		port := 0
		if dep.Port != nil {
			port = *dep.Port
		}
		s := fmt.Sprintf("%s:%d", domain, port)
		return &s
	}
	scheme := "https"
	if dep.NoSSL {
		scheme = "http"
	}
	s := fmt.Sprintf("%s://%s.%s", scheme, dep.Subdomain, domain)
	return &s
}

// listAssets returns each file under <challenge>/assets/, sorted, as paths
// relative to the challenge root. Directories with only README.md /
// .gitkeep / dotfiles are treated as empty (return nil).
func listAssets(c *challenge.Challenge) []string {
	entries, err := os.ReadDir(c.AssetsDir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == ".gitkeep" || name == ".DS_Store" {
			continue
		}
		// README.md is intentionally skipped (it's an assets-folder placeholder).
		if name == "README.md" || name == "Readme.md" {
			continue
		}
		files = append(files, filepath.Join("assets", name))
	}
	if len(files) == 0 {
		return nil
	}
	sort.Strings(files)
	return files
}
