package generate

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// indexDoc mirrors the public docs/spec/derived-files.md "index.json" shape.
// Field order is the rendered order — encoding/json honors struct order.
//
// Note: the JSON document is intentionally timestamp-free so `mtcf generate`
// is byte-deterministic across runs — this is what lets `--check` work in CI.
type indexDoc struct {
	Schema     int          `json:"schema"`
	Event      string       `json:"event"`
	Challenges []indexEntry `json:"challenges"`
}

type indexEntry struct {
	Category   string              `json:"category"`
	Slug       string              `json:"slug"`
	Name       string              `json:"name"`
	Author     string              `json:"author"`
	Difficulty string              `json:"difficulty"`
	Wave       int                 `json:"wave"`
	Archetype  challenge.Archetype `json:"archetype"`
	Deploy     *indexDeploy        `json:"deploy,omitempty"`
	Image      string              `json:"image,omitempty"`
	FlagCount  int                 `json:"flag_count"`
	HasWriteup bool                `json:"has_writeup"`
	HasHints   bool                `json:"has_hints"`
	AssetCount int                 `json:"asset_count"`
}

type indexDeploy struct {
	Subdomain    string `json:"subdomain,omitempty"`
	Port         *int   `json:"port,omitempty"`
	InternalPort *int   `json:"internal_port,omitempty"`
	TCP          bool   `json:"tcp,omitempty"`
}

// IndexJSON renders the machine-parseable event index.
func IndexJSON(cfg *config.Config, challs []*challenge.Challenge) ([]byte, error) {
	doc := indexDoc{Schema: 1}
	if cfg != nil {
		doc.Event = cfg.Event.Slug
	}
	for _, c := range challs {
		m := c.Manifest
		entry := indexEntry{
			Category:   m.Challenge.Category,
			Slug:       c.Slug,
			Name:       m.Challenge.Name,
			Author:     m.Challenge.Author,
			Difficulty: m.Challenge.Difficulty,
			Wave:       m.Challenge.Wave,
			Archetype:  c.Archetype,
			FlagCount:  len(m.Challenge.Flags),
			HasWriteup: c.HasWriteup(),
			HasHints:   len(m.Challenge.Hints) > 0 || fileExists(c.SolutionDir+"/hints.md"),
			AssetCount: len(listAssets(c)),
		}
		if m.Deploy != nil {
			entry.Deploy = &indexDeploy{
				Subdomain:    m.Deploy.Subdomain,
				Port:         m.Deploy.Port,
				InternalPort: m.Deploy.InternalPort,
				TCP:          m.Deploy.TCP,
			}
		}
		doc.Challenges = append(doc.Challenges, entry)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fileExists is also used by csv.go (kept here for the package).
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
