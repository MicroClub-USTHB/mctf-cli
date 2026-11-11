package webhook

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

// PayloadSchema is the version of the webhook payload contract
// documented in docs/spec/platforms.md.
const PayloadSchema = 1

// Envelope is the common header of every webhook payload.
type Envelope struct {
	Schema    int    `json:"schema"`
	Event     string `json:"event"`
	EventSlug string `json:"event_slug"`
	SentAt    string `json:"sent_at"`
}

func newEnvelope(event, eventSlug string, now time.Time) Envelope {
	return Envelope{
		Schema:    PayloadSchema,
		Event:     event,
		EventSlug: eventSlug,
		SentAt:    now.UTC().Format(time.RFC3339),
	}
}

// SyncPayload is a full snapshot of the (filtered) challenge set.
type SyncPayload struct {
	Envelope
	Challenges []Challenge `json:"challenges"`
}

// Challenge is one challenge entry in a sync payload.
type Challenge struct {
	Category       string              `json:"category"`
	Slug           string              `json:"slug"`
	Name           string              `json:"name"`
	Author         string              `json:"author"`
	Difficulty     string              `json:"difficulty"`
	Wave           int                 `json:"wave"`
	Archetype      challenge.Archetype `json:"archetype"`
	Hidden         bool                `json:"hidden"`
	Description    string              `json:"description"`
	Scoring        Scoring             `json:"scoring"`
	Flags          []Flag              `json:"flags"`
	Tags           []string            `json:"tags"`
	Hints          []Hint              `json:"hints"`
	ConnectionInfo string              `json:"connection_info"`
	Files          []File              `json:"files"`
}

// Scoring carries points for static scoring, or initial/decay/minimum
// for dynamic scoring.
type Scoring struct {
	Type    string `json:"type"`
	Points  int    `json:"points,omitempty"`
	Initial int    `json:"initial,omitempty"`
	Decay   int    `json:"decay,omitempty"`
	Minimum int    `json:"minimum,omitempty"`
}

// Flag is one accepted flag.
type Flag struct {
	Flag          string `json:"flag"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// Hint is one hint and its unlock cost.
type Hint struct {
	Content string `json:"content"`
	Cost    int    `json:"cost"`
}

// File identifies one asset by name and content hash; the bytes are
// not sent.
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// UnsyncPayload announces that one challenge was removed.
type UnsyncPayload struct {
	Envelope
	Category string `json:"category"`
	Slug     string `json:"slug"`
}

// ConfigurePayload carries event-level metadata.
type ConfigurePayload struct {
	Envelope
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	StartAt     string `json:"start_at,omitempty"`
	EndAt       string `json:"end_at,omitempty"`
}

// BuildSync builds the sync snapshot. Filters match the category
// directory and slug, like the CTFd planner; manifests without a name
// are skipped. Entries are sorted by category/slug.
func BuildSync(cfg *config.Config, challs []*challenge.Challenge, category, slug string, now time.Time) SyncPayload {
	out := SyncPayload{
		Envelope:   newEnvelope("sync", cfg.Event.Slug, now),
		Challenges: []Challenge{},
	}
	for _, c := range challs {
		m := c.Manifest.Challenge
		if m.Name == "" {
			continue
		}
		if category != "" && c.CategoryDir != category {
			continue
		}
		if slug != "" && c.Slug != slug {
			continue
		}
		s := platform.ResolveScoring(cfg, m.Scoring)
		entry := Challenge{
			Category:       m.Category,
			Slug:           c.Slug,
			Name:           m.Name,
			Author:         m.Author,
			Difficulty:     m.Difficulty,
			Wave:           m.Wave,
			Archetype:      c.Archetype,
			Hidden:         m.ForceHidden,
			Description:    platform.DescriptionWithAuthor(m.Description, m.Author),
			Scoring:        Scoring{Type: s.Type, Points: s.Points, Initial: s.Initial, Decay: s.Decay, Minimum: s.Minimum},
			Flags:          []Flag{},
			Tags:           []string{fmt.Sprintf("wave %d", m.Wave)},
			Hints:          []Hint{},
			ConnectionInfo: platform.ConnectionInfo(cfg, c),
			Files:          assetFiles(c.AssetsDir),
		}
		for _, f := range m.Flags {
			entry.Flags = append(entry.Flags, Flag{Flag: f.Flag, CaseSensitive: f.CaseSensitive})
		}
		entry.Tags = append(entry.Tags, m.Tags...)
		for _, h := range m.Hints {
			entry.Hints = append(entry.Hints, Hint{Content: h.Content, Cost: h.Cost})
		}
		out.Challenges = append(out.Challenges, entry)
	}
	sort.Slice(out.Challenges, func(i, j int) bool {
		a, b := out.Challenges[i], out.Challenges[j]
		return a.Category+"/"+a.Slug < b.Category+"/"+b.Slug
	})
	return out
}

// assetFiles lists dir's regular files (no subdirectories, no
// dotfiles), sorted by name. A file that cannot be read gets an empty
// sha256 rather than failing the whole payload.
func assetFiles(dir string) []File {
	out := []File{}
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sum, _ := sha256File(filepath.Join(dir, e.Name()))
		out = append(out, File{Name: e.Name(), SHA256: sum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
