package validate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Run runs every cross-cutting check from docs/spec/validation.md against
// the discovered challenges and the loaded event config.
//
// Schema-level errors (S001..S010, A001..A005) are reported by the loaders
// in internal/{config,challenge}; loadErrs threads those through here so
// the user sees them in one report.
func Run(cfg *config.Config, eventRoot string, challs []*challenge.Challenge, loadErrs map[string]error) *Report {
	r := &Report{}
	for path, err := range loadErrs {
		r.Errorf("S002", path, "manifest failed schema validation:\n%s", err)
	}
	cats := ResolveCategories(cfg)
	for _, c := range challs {
		if c.Manifest.Challenge.Name == "" {
			// Manifest didn't load — already reported via loadErrs.
			continue
		}
		checkOne(r, cfg, cats, c, eventRoot)
	}
	checkUniqueness(r, challs)
	return r
}

// checkOne runs the per-challenge rules (those that don't need event-wide context).
func checkOne(r *Report, cfg *config.Config, cats map[string]CategoryInfo, c *challenge.Challenge, eventRoot string) {
	m := c.Manifest
	manifestPath := relTo(eventRoot, c.ManifestPath())

	// S003 — category dir match
	if m.Challenge.Category != c.CategoryDir {
		r.Errorf("S003", manifestPath,
			"category %q does not match parent directory %q", m.Challenge.Category, c.CategoryDir)
	}

	// S004 — category in active catalog
	cat, known := cats[m.Challenge.Category]
	if !known {
		r.Errorf("S004", manifestPath,
			"category %q is not in the active catalog (core + extended + custom)", m.Challenge.Category)
	}

	// S008 — flag prefix
	prefix := "mctf"
	if cfg != nil && cfg.Event.FlagPrefix != "" {
		prefix = cfg.Event.FlagPrefix
	}
	flagRe := regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `\{[^}]+\}$`)
	for i, f := range m.Challenge.Flags {
		if !flagRe.MatchString(f.Flag) {
			r.Errorf("S008", manifestPath,
				"flags[%d].flag %q does not match the event flag prefix %q",
				i, f.Flag, prefix+"{...}")
		}
	}

	// S010 — wave bounds
	wmax := 3
	if cfg != nil && cfg.Event.WavesNumber > 0 {
		wmax = cfg.Event.WavesNumber
	}
	if m.Challenge.Wave < 1 || m.Challenge.Wave > wmax {
		r.Errorf("S010", manifestPath,
			"wave %d outside event range 1..%d", m.Challenge.Wave, wmax)
	}

	// S011 — directory name charset (also enforced at filesystem layer; defensive)
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(c.Slug) {
		r.Errorf("S011", manifestPath, "directory name %q must match [A-Za-z0-9_-]+", c.Slug)
	}

	// W005 — slug-vs-name case mismatch
	if challenge.SlugFromName(m.Challenge.Name) != strings.ToLower(c.Slug) {
		r.Warnf("W005", manifestPath,
			"directory %q does not match name %q (case/punctuation diff)", c.Slug, m.Challenge.Name)
	}

	archetype := challenge.DeriveArchetype(m)
	deployable := known && cat.Deployable

	// A001 — need_deploy=true requires deploy block (already enforced by schema's allOf,
	// but a friendlier rule code helps if someone ever bypasses schema).
	if m.Challenge.NeedDeploy && m.Deploy == nil {
		r.Errorf("A001", manifestPath, "need_deploy is true but no deploy block declared")
	}

	// A006 — deployable category needs Dockerfile + compose
	if archetype != challenge.ArchetypeStatic {
		if !c.HasDockerfile() {
			r.Errorf("A006", manifestPath, "deployable challenge missing challenge/Dockerfile")
		}
		if !c.HasCompose() {
			r.Errorf("A006", manifestPath, "deployable challenge missing challenge/docker-compose.yml")
		}
	}
	if deployable && !m.Challenge.NeedDeploy {
		r.Warnf("W001", manifestPath,
			"category %q is typically deployable but need_deploy is false", m.Challenge.Category)
	}

	// SOL001 — solution/writeup.md required and non-empty
	if !c.HasWriteup() {
		r.Errorf("SOL001", relTo(eventRoot, filepath.Join(c.SolutionDir, "writeup.md")),
			"missing or empty solution/writeup.md")
	}

	// D006 — service-archetype port within category range
	if archetype == challenge.ArchetypeService && m.Deploy != nil && m.Deploy.Port != nil && known && !cat.Range.Empty() {
		if !cat.Range.Contains(*m.Deploy.Port) {
			r.Warnf("D006", manifestPath,
				"deploy.port %d outside %s range %d-%d (use a port in the category slice)",
				*m.Deploy.Port, m.Challenge.Category, cat.Range.Min, cat.Range.Max)
		}
	}

	// D004 — instanced archetype needs a healthcheck
	if archetype == challenge.ArchetypeInstanced {
		hasHC := (m.Deploy != nil && m.Deploy.Healthcheck != nil) || hasDockerfileHealthcheck(c.ChallengeDir)
		if !hasHC {
			r.Errorf("D004", manifestPath,
				"instanced challenge has no healthcheck (set deploy.healthcheck or add a HEALTHCHECK to the Dockerfile)")
		}
	}

	// D005 — instanced archetype needs CPU and memory limits
	if archetype == challenge.ArchetypeInstanced {
		if m.Deploy == nil || m.Deploy.Resources == nil || m.Deploy.Resources.CPU == "" || m.Deploy.Resources.Memory == "" {
			r.Warnf("D005", manifestPath,
				"instanced challenge should declare deploy.resources.cpu and deploy.resources.memory")
		}
	}

	// W004 — static archetype with empty assets/
	if archetype == challenge.ArchetypeStatic {
		if isEmptyDir(c.AssetsDir) {
			r.Warnf("W004", relTo(eventRoot, c.AssetsDir),
				"static challenge has no assets/ — what do players download?")
		}
	}

	// SEC001 — image-leak guard: static flag value found in challenge/src/
	for _, f := range m.Challenge.Flags {
		if f.Flag == "" {
			continue
		}
		if hits := scanForFlag(filepath.Join(c.ChallengeDir, "src"), f.Flag); len(hits) > 0 {
			for _, hit := range hits {
				r.Errorf("SEC001", relTo(eventRoot, hit),
					"static flag value found in source — bake it into flag.txt or load via env to avoid image-layer leak")
			}
		}
	}

	// SEC002–004, W006: privileged-mode policy, dangerous capabilities,
	// pwn/jailbreak non-root user, asset-size limits.
	SecurityChecks(r, cfg, c, eventRoot)

	// W010 — description length sanity. CTFd's challenge description
	// column tolerates ~4000 chars before truncating; long descriptions
	// also hurt readability on the platform UI. Warn well below that.
	if n := len(m.Challenge.Description); n > 4000 {
		r.Errorf("W010", manifestPath,
			"description is %d chars — CTFd truncates near 4000", n)
	} else if n > 1500 {
		r.Warnf("W010", manifestPath,
			"description is %d chars — consider trimming for UX (hard cap at 4000)", n)
	}
}

// checkUniqueness runs the event-wide cross checks (U001/U002/U003).
func checkUniqueness(r *Report, challs []*challenge.Challenge) {
	seenPort := map[int]string{}      // port -> first challenge slug
	seenSubdomain := map[string]string{}
	seenFlag := map[string]string{}

	for _, c := range challs {
		key := c.CategoryDir + "/" + c.Slug
		m := c.Manifest
		if m.Deploy != nil {
			if m.Deploy.Port != nil {
				if other, ok := seenPort[*m.Deploy.Port]; ok {
					r.Errorf("U001", c.ManifestPath(),
						"deploy.port %d also used by %s", *m.Deploy.Port, other)
				} else {
					seenPort[*m.Deploy.Port] = key
				}
			}
			if sd := m.Deploy.Subdomain; sd != "" {
				if other, ok := seenSubdomain[sd]; ok {
					r.Errorf("U002", c.ManifestPath(),
						"deploy.subdomain %q also used by %s", sd, other)
				} else {
					seenSubdomain[sd] = key
				}
			}
		}
		for _, f := range m.Challenge.Flags {
			fkey := f.Flag
			if !f.CaseSensitive {
				fkey = strings.ToLower(fkey)
			}
			if other, ok := seenFlag[fkey]; ok {
				r.Errorf("U003", c.ManifestPath(),
					"flag %q also used by %s", f.Flag, other)
			} else {
				seenFlag[fkey] = key
			}
		}
	}
}

func hasDockerfileHealthcheck(challengeDir string) bool {
	data, err := os.ReadFile(filepath.Join(challengeDir, "Dockerfile"))
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`(?im)^\s*HEALTHCHECK\b`)
	return re.Match(data)
}

func isEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		// Tolerate placeholder .gitkeep / README.md
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		return false
	}
	return true
}

// scanForFlag returns relative file paths under root that contain needle.
// Files exempt from the leak guard are flag.txt and *.flag.
func scanForFlag(root, needle string) []string {
	var hits []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := strings.ToLower(d.Name())
		if base == "flag.txt" || strings.HasSuffix(base, ".flag") {
			return nil
		}
		// Only scan reasonably small text-ish files to avoid huge binaries.
		fi, err := d.Info()
		if err != nil || fi.Size() > 1<<20 { // 1 MiB
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), needle) {
			hits = append(hits, p)
		}
		return nil
	})
	return hits
}

func relTo(root, path string) string {
	if root == "" {
		return path
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// ensure fmt is used (kept for future formatting)
var _ = fmt.Sprintf
