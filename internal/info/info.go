// Package info renders human- and machine-readable summaries of one
// challenge: the derived archetype, ingress URLs, image-tag template,
// flag, and file inventory.
//
// This is a presentation layer — it never mutates state. Callers
// supply the loaded config + a single Challenge.
package info

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// View is the structured view of one challenge — both consumed by the
// pretty renderer and emitted as JSON when --json is passed.
type View struct {
	Category    string   `json:"category"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Author      string   `json:"author"`
	Difficulty  string   `json:"difficulty"`
	Wave        int      `json:"wave"`
	Archetype   string   `json:"archetype"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`

	Flag struct {
		Value         string `json:"value"`
		CaseSensitive bool   `json:"case_sensitive"`
	} `json:"flag"`

	Build struct {
		Image       string `json:"image,omitempty"`
		ImageLatest string `json:"image_latest,omitempty"`
	} `json:"build,omitempty"`

	Ingress *Ingress `json:"ingress,omitempty"`

	Files Files `json:"files"`

	Hints []Hint `json:"hints,omitempty"`
}

// Ingress describes how players reach the challenge at runtime.
type Ingress struct {
	Mode         string `json:"mode"`
	URL          string `json:"url,omitempty"`
	Subdomain    string `json:"subdomain,omitempty"`
	Port         int    `json:"port,omitempty"`
	InternalPort int    `json:"internal_port,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
}

// Files inventories the four per-challenge folders.
type Files struct {
	HasDockerfile bool     `json:"has_dockerfile"`
	HasCompose    bool     `json:"has_compose"`
	HasWriteup    bool     `json:"has_writeup"`
	HasHints      bool     `json:"has_hints"`
	SrcCount      int      `json:"src_count"`
	Assets        []string `json:"assets,omitempty"`
	Other         []string `json:"other,omitempty"`
}

// Hint is a cost-tiered hint.
type Hint struct {
	Cost    int    `json:"cost"`
	Content string `json:"content"`
}

// Build constructs a View for a challenge given the event config.
func Build(cfg *config.Config, c *challenge.Challenge) View {
	v := View{
		Category:    c.Manifest.Challenge.Category,
		Slug:        c.Slug,
		Name:        c.Manifest.Challenge.Name,
		Author:      c.Manifest.Challenge.Author,
		Difficulty:  c.Manifest.Challenge.Difficulty,
		Wave:        c.Manifest.Challenge.Wave,
		Archetype:   string(c.Archetype),
		Description: c.Manifest.Challenge.Description,
		Tags:        append([]string(nil), c.Manifest.Challenge.Tags...),
	}
	if len(c.Manifest.Challenge.Flags) > 0 {
		v.Flag.Value = c.Manifest.Challenge.Flags[0].Flag
		v.Flag.CaseSensitive = c.Manifest.Challenge.Flags[0].CaseSensitive
	}
	for _, h := range c.Manifest.Challenge.Hints {
		v.Hints = append(v.Hints, Hint{Cost: h.Cost, Content: h.Content})
	}

	v.Files = inventoryFiles(c)

	// Image tag derivation matches the spec: <registry>/<event>-<cat>-<slug>:<tag>.
	if cfg.Registry != nil && cfg.Registry.URL != "" && c.Manifest.Challenge.NeedDeploy {
		base := fmt.Sprintf("%s/%s-%s-%s",
			strings.TrimRight(cfg.Registry.URL, "/"),
			cfg.Event.Slug, v.Category, v.Slug,
		)
		v.Build.Image = base + ":<sha>"
		v.Build.ImageLatest = base + ":latest"
	}

	v.Ingress = deriveIngress(cfg, c)
	return v
}

func deriveIngress(cfg *config.Config, c *challenge.Challenge) *Ingress {
	if !c.Manifest.Challenge.NeedDeploy || c.Manifest.Deploy == nil {
		return nil
	}
	d := c.Manifest.Deploy
	domain := strings.TrimSpace(cfg.Event.Domain)
	ing := &Ingress{Subdomain: d.Subdomain, Protocol: ingressProtocol(d.TCP, d.NoSSL)}
	switch c.Archetype {
	case challenge.ArchetypeService:
		if d.Port != nil {
			ing.Port = *d.Port
		}
		ing.Mode = "service"
		ing.URL = serviceURL(domain, d, ing.Port)
	case challenge.ArchetypeInstanced:
		ing.Mode = "instanced"
		if d.InternalPort != nil {
			ing.InternalPort = *d.InternalPort
		}
		if cfg.Instancer != nil && cfg.Instancer.URL != "" {
			ing.URL = cfg.Instancer.URL
		} else if domain != "" {
			ing.URL = "https://instancer." + domain
		}
	}
	return ing
}

func ingressProtocol(tcp, noSSL bool) string {
	switch {
	case tcp:
		return "tcp"
	case noSSL:
		return "http"
	default:
		return "https"
	}
}

func serviceURL(domain string, d *challenge.DeployBlock, port int) string {
	if domain == "" || d.Subdomain == "" {
		return ""
	}
	scheme := "https"
	if d.NoSSL {
		scheme = "http"
	}
	if d.TCP {
		if port == 0 {
			return ""
		}
		return fmt.Sprintf("nc %s.%s %d", d.Subdomain, domain, port)
	}
	if port != 0 {
		return fmt.Sprintf("%s://%s.%s:%d", scheme, d.Subdomain, domain, port)
	}
	return fmt.Sprintf("%s://%s.%s", scheme, d.Subdomain, domain)
}

func inventoryFiles(c *challenge.Challenge) Files {
	return Files{
		HasDockerfile: c.HasDockerfile(),
		HasCompose:    c.HasCompose(),
		HasWriteup:    c.HasWriteup(),
		HasHints:      fileExists(filepath.Join(c.SolutionDir, "hints.md")) || len(c.Manifest.Challenge.Hints) > 0,
		SrcCount:      countFiles(filepath.Join(c.ChallengeDir, "src")),
		Assets:        listFiles(c.AssetsDir),
		Other:         listFiles(c.OtherDir),
	}
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func listFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue // skip .gitkeep / .DS_Store
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// JSON returns the JSON-encoded view.
func (v View) JSON() ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Render produces a tabular human-readable rendering.
func (v View) Render() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s/%s\n", v.Category, v.Slug)

	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  name\t%s\n", v.Name)
	fmt.Fprintf(tw, "  author\t%s\n", v.Author)
	fmt.Fprintf(tw, "  difficulty\t%s\n", v.Difficulty)
	fmt.Fprintf(tw, "  wave\t%d\n", v.Wave)
	fmt.Fprintf(tw, "  archetype\t%s\n", v.Archetype)
	if len(v.Tags) > 0 {
		fmt.Fprintf(tw, "  tags\t%s\n", strings.Join(v.Tags, ", "))
	}
	tw.Flush()

	if v.Description != "" {
		sb.WriteString("\n  description\n")
		for _, line := range strings.Split(strings.TrimRight(v.Description, "\n"), "\n") {
			sb.WriteString("    " + line + "\n")
		}
	}

	cs := "false"
	if v.Flag.CaseSensitive {
		cs = "true"
	}
	fmt.Fprintf(&sb, "\n  flag (case sensitive: %s)\n    %s\n", cs, v.Flag.Value)

	if v.Ingress != nil {
		sb.WriteString("\n  ingress\n")
		tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "    mode\t%s\n", v.Ingress.Mode)
		if v.Ingress.URL != "" {
			fmt.Fprintf(tw, "    url\t%s\n", v.Ingress.URL)
		}
		if v.Ingress.Subdomain != "" {
			fmt.Fprintf(tw, "    subdomain\t%s\n", v.Ingress.Subdomain)
		}
		if v.Ingress.Port != 0 {
			fmt.Fprintf(tw, "    port\t%d\n", v.Ingress.Port)
		}
		if v.Ingress.InternalPort != 0 {
			fmt.Fprintf(tw, "    internal_port\t%d\n", v.Ingress.InternalPort)
		}
		fmt.Fprintf(tw, "    protocol\t%s\n", v.Ingress.Protocol)
		tw.Flush()
	}

	if v.Build.Image != "" {
		sb.WriteString("\n  build\n")
		tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "    image\t%s\n", v.Build.Image)
		fmt.Fprintf(tw, "    image:latest\t%s\n", v.Build.ImageLatest)
		tw.Flush()
	}

	sb.WriteString("\n  files\n")
	tw = tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "    Dockerfile\t%s\n", checkmark(v.Files.HasDockerfile))
	fmt.Fprintf(tw, "    docker-compose.yml\t%s\n", checkmark(v.Files.HasCompose))
	fmt.Fprintf(tw, "    src/\t%d file(s)\n", v.Files.SrcCount)
	fmt.Fprintf(tw, "    writeup.md\t%s\n", checkmark(v.Files.HasWriteup))
	fmt.Fprintf(tw, "    hints.md\t%s\n", checkmark(v.Files.HasHints))
	fmt.Fprintf(tw, "    assets/\t%s\n", filesList(v.Files.Assets))
	fmt.Fprintf(tw, "    other/\t%s\n", filesList(v.Files.Other))
	tw.Flush()

	if len(v.Hints) > 0 {
		sb.WriteString("\n  hints\n")
		for _, h := range v.Hints {
			fmt.Fprintf(&sb, "    [%d] %s\n", h.Cost, h.Content)
		}
	}

	return sb.String()
}

func checkmark(ok bool) string {
	if ok {
		return "yes"
	}
	return "—"
}

func filesList(files []string) string {
	if len(files) == 0 {
		return "(empty)"
	}
	if len(files) <= 4 {
		return fmt.Sprintf("%d (%s)", len(files), strings.Join(files, ", "))
	}
	return fmt.Sprintf("%d files", len(files))
}
