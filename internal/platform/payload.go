package platform

import (
	"fmt"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Scoring is the effective scoring for one challenge. Static scoring
// sets Points; dynamic scoring sets Initial, Decay and Minimum.
type Scoring struct {
	Type    string
	Points  int
	Initial int
	Decay   int
	Minimum int
}

// ResolveScoring picks per-challenge scoring or falls back to the event
// default, or to a static-100pt default. Mirrors generate.resolveScoring.
func ResolveScoring(cfg *config.Config, override *challenge.Scoring) Scoring {
	if override != nil && override.Type != "" {
		return scoringFrom(override.Type, override.Points, override.Initial, override.Decay, override.Minimum)
	}
	if cfg != nil && cfg.Event.Scoring != nil {
		s := cfg.Event.Scoring
		return scoringFrom(s.Type, s.Points, s.Initial, s.Decay, s.Minimum)
	}
	return Scoring{Type: "static", Points: 100}
}

func scoringFrom(t string, points, initial, decay, minimum int) Scoring {
	if t == "static" {
		if points == 0 {
			points = 100
		}
		return Scoring{Type: "static", Points: points}
	}
	if initial == 0 {
		initial = 500
	}
	if decay == 0 {
		decay = 10
	}
	if minimum == 0 {
		minimum = 100
	}
	return Scoring{Type: "dynamic", Initial: initial, Decay: decay, Minimum: minimum}
}

// DescriptionWithAuthor matches the CTFd-manifest generator output so
// the description sent over the wire equals what `mctf generate`
// would have written.
func DescriptionWithAuthor(desc, author string) string {
	desc = strings.TrimRight(desc, "\n")
	if author == "" {
		return desc
	}
	return desc + "\n\n**Author: " + author + "**"
}

// ConnectionInfo is the URL shown to players alongside the description.
// For instanced challenges it's the instancer URL; for service it's
// the public ingress URL; for static it's empty.
func ConnectionInfo(cfg *config.Config, c *challenge.Challenge) string {
	if !c.Manifest.Challenge.NeedDeploy || c.Manifest.Deploy == nil {
		return ""
	}
	if c.Archetype == challenge.ArchetypeInstanced {
		if cfg.Instancer != nil && cfg.Instancer.URL != "" {
			return cfg.Instancer.URL
		}
		if cfg.Event.Domain != "" {
			return "https://instancer." + cfg.Event.Domain
		}
		return ""
	}
	d := c.Manifest.Deploy
	if cfg.Event.Domain == "" || d.Subdomain == "" {
		return ""
	}
	scheme := "https"
	if d.NoSSL {
		scheme = "http"
	}
	if d.TCP {
		port := 0
		if d.Port != nil {
			port = *d.Port
		}
		if port == 0 {
			return ""
		}
		return fmt.Sprintf("nc %s.%s %d", d.Subdomain, cfg.Event.Domain, port)
	}
	if d.Port != nil {
		return fmt.Sprintf("%s://%s.%s:%d", scheme, d.Subdomain, cfg.Event.Domain, *d.Port)
	}
	return fmt.Sprintf("%s://%s.%s", scheme, d.Subdomain, cfg.Event.Domain)
}
