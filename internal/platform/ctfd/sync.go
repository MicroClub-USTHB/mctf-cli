package ctfd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Action is one resolved sync operation. The CLI prints these and the
// applier executes them in declaration order.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
)

// Item is one challenge's planned action.
type Item struct {
	Slug       string // <category>/<slug>
	Action     Action
	Reason     string    // human-readable why
	Payload    Challenge // CTFd-side challenge payload
	Flags      []FlagDelta
	Tags       []TagDelta
	Hints      []HintDelta
	Files      []FileDelta
	ExistingID int // non-zero on update

	// challengePtr threads the source challenge through to Apply so
	// it can resolve absolute paths for file uploads.
	challengePtr *challenge.Challenge
}

// FlagDelta describes a flag-level diff (replace-all semantics for v0).
type FlagDelta struct {
	Op       string // "add" | "delete"
	Existing *Flag  // for delete
	Wanted   *Flag  // for add
}

// TagDelta describes a tag-level diff.
type TagDelta struct {
	Op       string
	Existing *Tag
	Wanted   *Tag
}

// HintDelta describes a hint-level diff.
type HintDelta struct {
	Op       string
	Existing *Hint
	Wanted   *Hint
}

// FileDelta describes an asset-level diff. WantedPath is the absolute
// path on disk; Existing carries the CTFd-side file identity for
// deletes.
type FileDelta struct {
	Op         string // "add" | "delete"
	Existing   *File
	WantedName string // basename, used for display + sort
	WantedPath string // absolute local path (only set for "add")
}

// Plan is the full set of actions for a sync run.
type Plan struct {
	Items []Item
}

// PlanOptions narrow what the planner considers.
type PlanOptions struct {
	CategoryFilter string
	SlugFilter     string
	// AllowUpdate enables update of existing challenges. When false
	// (the default), existing challenges are reported as Skip with
	// reason "already-exists" — safer for first runs.
	AllowUpdate bool
}

// BuildPayload converts an author manifest into the CTFd Challenge
// payload that would be POSTed. Pure function — no network — so
// callers can inspect it in dry-run mode.
func BuildPayload(cfg *config.Config, c *challenge.Challenge) Challenge {
	m := c.Manifest
	out := Challenge{
		Name:        m.Challenge.Name,
		Category:    m.Challenge.Category,
		Description: descriptionWithAuthor(m.Challenge.Description, m.Challenge.Author),
		State:       "visible",
	}
	if m.Challenge.ForceHidden {
		out.State = "hidden"
	}

	scoring := resolveScoring(cfg, m.Challenge.Scoring)
	if scoring.Type == "static" {
		v := scoring.Points
		out.Type = "standard"
		out.Value = &v
	} else {
		out.Type = "dynamic"
		i, d, mn := scoring.Initial, scoring.Decay, scoring.Minimum
		out.Initial, out.Decay, out.Minimum = &i, &d, &mn
	}

	out.ConnectionInfo = connectionInfo(cfg, c)
	return out
}

// PayloadFlags returns the CTFd Flag list a manifest wants.
func PayloadFlags(c *challenge.Challenge) []Flag {
	var out []Flag
	for _, f := range c.Manifest.Challenge.Flags {
		flag := Flag{Type: "static", Content: f.Flag}
		if f.CaseSensitive {
			flag.Data = "case_sensitive"
		}
		out = append(out, flag)
	}
	return out
}

// PayloadTags returns the CTFd Tag list a manifest wants. The
// "wave N" tag is always emitted; explicit tags follow.
func PayloadTags(c *challenge.Challenge) []Tag {
	out := []Tag{{Value: fmt.Sprintf("wave %d", c.Manifest.Challenge.Wave)}}
	for _, tag := range c.Manifest.Challenge.Tags {
		out = append(out, Tag{Value: tag})
	}
	return out
}

// PayloadFiles enumerates the assets/ directory and returns one entry
// per file (as basename + absolute path). Hidden files and the
// .gitkeep placeholder are filtered out. Output is sorted.
func PayloadFiles(c *challenge.Challenge) []FileDelta {
	if c.AssetsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(c.AssetsDir)
	if err != nil {
		return nil
	}
	var out []FileDelta
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, FileDelta{
			Op:         "add",
			WantedName: e.Name(),
			WantedPath: filepath.Join(c.AssetsDir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WantedName < out[j].WantedName })
	return out
}

// PayloadHints returns the CTFd Hint list a manifest wants.
func PayloadHints(c *challenge.Challenge) []Hint {
	var out []Hint
	for _, h := range c.Manifest.Challenge.Hints {
		out = append(out, Hint{Content: h.Content, Cost: h.Cost})
	}
	return out
}

// Diff computes the per-challenge action set against the live CTFd
// state. It does not mutate; the caller passes the returned Plan to
// Apply (or prints it under --dry-run).
func Diff(ctx context.Context, c *Client, cfg *config.Config, challs []*challenge.Challenge, opts PlanOptions) (*Plan, error) {
	existing, err := c.ListChallenges(ctx)
	if err != nil {
		return nil, fmt.Errorf("list existing challenges: %w", err)
	}
	byKey := map[string]Challenge{}
	for _, ec := range existing {
		byKey[challengeKey(ec.Category, ec.Name)] = ec
	}

	var items []Item
	for _, ch := range challs {
		if ch.Manifest.Challenge.Name == "" {
			continue
		}
		if opts.CategoryFilter != "" && ch.CategoryDir != opts.CategoryFilter {
			continue
		}
		if opts.SlugFilter != "" && ch.Slug != opts.SlugFilter {
			continue
		}

		payload := BuildPayload(cfg, ch)
		key := challengeKey(payload.Category, payload.Name)

		if ec, ok := byKey[key]; ok {
			if !opts.AllowUpdate {
				items = append(items, Item{
					Slug:         ch.CategoryDir + "/" + ch.Slug,
					Action:       ActionSkip,
					Reason:       fmt.Sprintf("already exists (id=%d) — re-run with --update to reconcile", ec.ID),
					Payload:      payload,
					ExistingID:   ec.ID,
					challengePtr: ch,
				})
				continue
			}
			// Update path: compute deltas for flags/tags/hints/files.
			flagDeltas, tagDeltas, hintDeltas, fileDeltas, err := childDeltas(ctx, c, ec.ID, ch)
			if err != nil {
				return nil, err
			}
			items = append(items, Item{
				Slug:         ch.CategoryDir + "/" + ch.Slug,
				Action:       ActionUpdate,
				Reason:       "exists; reconciling fields and child resources",
				Payload:      payload,
				ExistingID:   ec.ID,
				Flags:        flagDeltas,
				Tags:         tagDeltas,
				Hints:        hintDeltas,
				Files:        fileDeltas,
				challengePtr: ch,
			})
		} else {
			items = append(items, Item{
				Slug:         ch.CategoryDir + "/" + ch.Slug,
				Action:       ActionCreate,
				Reason:       "missing on platform",
				Payload:      payload,
				Flags:        wantFlagDeltas(PayloadFlags(ch)),
				Tags:         wantTagDeltas(PayloadTags(ch)),
				Hints:        wantHintDeltas(PayloadHints(ch)),
				Files:        PayloadFiles(ch),
				challengePtr: ch,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Slug < items[j].Slug })
	return &Plan{Items: items}, nil
}

// Apply executes plan against the CTFd API. Returns the first error
// encountered; on success returns the number of items applied.
func Apply(ctx context.Context, c *Client, plan *Plan) (int, error) {
	applied := 0
	for _, item := range plan.Items {
		switch item.Action {
		case ActionSkip:
			continue
		case ActionCreate:
			created, err := c.CreateChallenge(ctx, item.Payload)
			if err != nil {
				return applied, fmt.Errorf("%s: create challenge: %w", item.Slug, err)
			}
			if err := applyChildResources(ctx, c, created.ID, item); err != nil {
				return applied, err
			}
		case ActionUpdate:
			if _, err := c.UpdateChallenge(ctx, item.ExistingID, item.Payload); err != nil {
				return applied, fmt.Errorf("%s: update challenge: %w", item.Slug, err)
			}
			if err := applyChildResources(ctx, c, item.ExistingID, item); err != nil {
				return applied, err
			}
		}
		applied++
	}
	return applied, nil
}

func applyChildResources(ctx context.Context, c *Client, challengeID int, item Item) error {
	for _, fd := range item.Flags {
		switch fd.Op {
		case "add":
			cs := fd.Wanted.Data == "case_sensitive"
			if _, err := c.AddFlag(ctx, challengeID, fd.Wanted.Content, cs); err != nil {
				return fmt.Errorf("%s: add flag: %w", item.Slug, err)
			}
		case "delete":
			if err := c.DeleteFlag(ctx, fd.Existing.ID); err != nil {
				return fmt.Errorf("%s: delete flag %d: %w", item.Slug, fd.Existing.ID, err)
			}
		}
	}
	for _, td := range item.Tags {
		switch td.Op {
		case "add":
			if _, err := c.AddTag(ctx, challengeID, td.Wanted.Value); err != nil {
				return fmt.Errorf("%s: add tag: %w", item.Slug, err)
			}
		case "delete":
			if err := c.DeleteTag(ctx, td.Existing.ID); err != nil {
				return fmt.Errorf("%s: delete tag %d: %w", item.Slug, td.Existing.ID, err)
			}
		}
	}
	for _, hd := range item.Hints {
		switch hd.Op {
		case "add":
			if _, err := c.AddHint(ctx, challengeID, hd.Wanted.Cost, hd.Wanted.Content); err != nil {
				return fmt.Errorf("%s: add hint: %w", item.Slug, err)
			}
		case "delete":
			if err := c.DeleteHint(ctx, hd.Existing.ID); err != nil {
				return fmt.Errorf("%s: delete hint %d: %w", item.Slug, hd.Existing.ID, err)
			}
		}
	}
	for _, fd := range item.Files {
		switch fd.Op {
		case "add":
			f, err := os.Open(fd.WantedPath)
			if err != nil {
				return fmt.Errorf("%s: open asset %s: %w", item.Slug, fd.WantedName, err)
			}
			_, uploadErr := c.UploadFile(ctx, challengeID, fd.WantedName, f)
			f.Close()
			if uploadErr != nil {
				return fmt.Errorf("%s: upload %s: %w", item.Slug, fd.WantedName, uploadErr)
			}
		case "delete":
			if err := c.DeleteFile(ctx, fd.Existing.ID); err != nil {
				return fmt.Errorf("%s: delete file %d: %w", item.Slug, fd.Existing.ID, err)
			}
		}
	}
	return nil
}

// childDeltas computes flag/tag/hint/file deltas for an existing
// challenge: add what's wanted-but-missing, delete what's existing-
// but-unwanted.
func childDeltas(ctx context.Context, c *Client, existingID int, ch *challenge.Challenge) ([]FlagDelta, []TagDelta, []HintDelta, []FileDelta, error) {
	flags, err := c.ListFlags(ctx, existingID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tags, err := c.ListTags(ctx, existingID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	hints, err := c.ListHints(ctx, existingID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	files, err := c.ListFiles(ctx, existingID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	wantFlags := PayloadFlags(ch)
	wantTags := PayloadTags(ch)
	wantHints := PayloadHints(ch)
	wantFiles := PayloadFiles(ch)

	flagDeltas := diffFlags(flags, wantFlags)
	tagDeltas := diffTags(tags, wantTags)
	hintDeltas := diffHints(hints, wantHints)
	fileDeltas := diffFiles(files, wantFiles)
	return flagDeltas, tagDeltas, hintDeltas, fileDeltas, nil
}

// diffFiles matches local assets to CTFd-side files by basename. Local
// content changes are not detected (CTFd hashes server-side); to force
// a re-upload, delete the file in CTFd first.
func diffFiles(have []File, want []FileDelta) []FileDelta {
	wantIdx := map[string]FileDelta{}
	for _, f := range want {
		wantIdx[f.WantedName] = f
	}
	haveIdx := map[string]File{}
	for _, f := range have {
		haveIdx[f.Basename()] = f
	}
	var out []FileDelta
	for name, f := range wantIdx {
		if _, ok := haveIdx[name]; !ok {
			out = append(out, f)
		}
	}
	for name, f := range haveIdx {
		if _, ok := wantIdx[name]; !ok {
			f := f
			out = append(out, FileDelta{Op: "delete", Existing: &f, WantedName: name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ki := fileSortKey(out[i])
		kj := fileSortKey(out[j])
		return ki < kj
	})
	return out
}

func fileSortKey(d FileDelta) string {
	if d.Op == "delete" {
		return "d:" + d.WantedName
	}
	return "a:" + d.WantedName
}

func diffFlags(have, want []Flag) []FlagDelta {
	wantIdx := map[string]Flag{}
	for _, f := range want {
		wantIdx[f.Content+"|"+f.Data] = f
	}
	haveIdx := map[string]Flag{}
	for _, f := range have {
		haveIdx[f.Content+"|"+f.Data] = f
	}
	var out []FlagDelta
	// Add what's wanted-but-missing.
	for k, f := range wantIdx {
		if _, ok := haveIdx[k]; !ok {
			f := f
			out = append(out, FlagDelta{Op: "add", Wanted: &f})
		}
	}
	// Delete what's existing-but-unwanted.
	for k, f := range haveIdx {
		if _, ok := wantIdx[k]; !ok {
			f := f
			out = append(out, FlagDelta{Op: "delete", Existing: &f})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ki := flagKey(out[i].Wanted, out[i].Existing)
		kj := flagKey(out[j].Wanted, out[j].Existing)
		return ki < kj
	})
	return out
}

func flagKey(w, e *Flag) string {
	if w != nil {
		return "a:" + w.Content
	}
	if e != nil {
		return "d:" + e.Content
	}
	return ""
}

func diffTags(have, want []Tag) []TagDelta {
	wantIdx := map[string]Tag{}
	for _, t := range want {
		wantIdx[t.Value] = t
	}
	haveIdx := map[string]Tag{}
	for _, t := range have {
		haveIdx[t.Value] = t
	}
	var out []TagDelta
	for v, t := range wantIdx {
		if _, ok := haveIdx[v]; !ok {
			t := t
			out = append(out, TagDelta{Op: "add", Wanted: &t})
		}
	}
	for v, t := range haveIdx {
		if _, ok := wantIdx[v]; !ok {
			t := t
			out = append(out, TagDelta{Op: "delete", Existing: &t})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ki := tagKey(out[i].Wanted, out[i].Existing)
		kj := tagKey(out[j].Wanted, out[j].Existing)
		return ki < kj
	})
	return out
}

func tagKey(w, e *Tag) string {
	if w != nil {
		return "a:" + w.Value
	}
	if e != nil {
		return "d:" + e.Value
	}
	return ""
}

func diffHints(have, want []Hint) []HintDelta {
	wantIdx := map[string]Hint{}
	for _, h := range want {
		wantIdx[hintIdent(h)] = h
	}
	haveIdx := map[string]Hint{}
	for _, h := range have {
		haveIdx[hintIdent(h)] = h
	}
	var out []HintDelta
	for k, h := range wantIdx {
		if _, ok := haveIdx[k]; !ok {
			h := h
			out = append(out, HintDelta{Op: "add", Wanted: &h})
		}
	}
	for k, h := range haveIdx {
		if _, ok := wantIdx[k]; !ok {
			h := h
			out = append(out, HintDelta{Op: "delete", Existing: &h})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ki := hintSortKey(out[i].Wanted, out[i].Existing)
		kj := hintSortKey(out[j].Wanted, out[j].Existing)
		return ki < kj
	})
	return out
}

func hintIdent(h Hint) string {
	return fmt.Sprintf("%d|%s", h.Cost, h.Content)
}

func hintSortKey(w, e *Hint) string {
	if w != nil {
		return "a:" + hintIdent(*w)
	}
	if e != nil {
		return "d:" + hintIdent(*e)
	}
	return ""
}

func wantFlagDeltas(fs []Flag) []FlagDelta {
	out := make([]FlagDelta, 0, len(fs))
	for i := range fs {
		f := fs[i]
		out = append(out, FlagDelta{Op: "add", Wanted: &f})
	}
	return out
}

func wantTagDeltas(ts []Tag) []TagDelta {
	out := make([]TagDelta, 0, len(ts))
	for i := range ts {
		t := ts[i]
		out = append(out, TagDelta{Op: "add", Wanted: &t})
	}
	return out
}

func wantHintDeltas(hs []Hint) []HintDelta {
	out := make([]HintDelta, 0, len(hs))
	for i := range hs {
		h := hs[i]
		out = append(out, HintDelta{Op: "add", Wanted: &h})
	}
	return out
}

func challengeKey(category, name string) string {
	return strings.ToLower(category) + "::" + strings.ToLower(name)
}

// descriptionWithAuthor matches the CTFd-manifest generator output so
// the description sent over the wire equals what `mctf generate`
// would have written.
func descriptionWithAuthor(desc, author string) string {
	desc = strings.TrimRight(desc, "\n")
	if author == "" {
		return desc
	}
	return desc + "\n\n**Author: " + author + "**"
}

// connectionInfo is the URL CTFd shows alongside the description.
// For instanced challenges it's the instancer URL; for service it's
// the public ingress URL; for static it's empty.
func connectionInfo(cfg *config.Config, c *challenge.Challenge) string {
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

// resolveScoring picks per-challenge scoring or falls back to event default,
// or to a static-100pt default. Mirrors generate.resolveScoring.
type resolvedScoring struct {
	Type    string
	Points  int
	Initial int
	Decay   int
	Minimum int
}

func resolveScoring(cfg *config.Config, override *challenge.Scoring) resolvedScoring {
	if override != nil && override.Type != "" {
		return scoringFrom(override.Type, override.Points, override.Initial, override.Decay, override.Minimum)
	}
	if cfg != nil && cfg.Event.Scoring != nil {
		s := cfg.Event.Scoring
		return scoringFrom(s.Type, s.Points, s.Initial, s.Decay, s.Minimum)
	}
	return resolvedScoring{Type: "static", Points: 100}
}

func scoringFrom(t string, points, initial, decay, minimum int) resolvedScoring {
	if t == "static" {
		if points == 0 {
			points = 100
		}
		return resolvedScoring{Type: "static", Points: points}
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
	return resolvedScoring{Type: "dynamic", Initial: initial, Decay: decay, Minimum: minimum}
}

// Render produces a human-readable summary of the plan.
func (p *Plan) Render() string {
	var sb strings.Builder
	for _, item := range p.Items {
		fmt.Fprintf(&sb, "  [%-6s] %s — %s\n", item.Action, item.Slug, item.Reason)
		for _, fd := range item.Flags {
			fmt.Fprintf(&sb, "          flag.%s   %s\n", fd.Op, flagDisplay(fd))
		}
		for _, td := range item.Tags {
			fmt.Fprintf(&sb, "          tag.%s    %s\n", td.Op, tagDisplay(td))
		}
		for _, hd := range item.Hints {
			fmt.Fprintf(&sb, "          hint.%s   %s\n", hd.Op, hintDisplay(hd))
		}
		for _, fd := range item.Files {
			fmt.Fprintf(&sb, "          file.%s   %s\n", fd.Op, fd.WantedName)
		}
	}
	nCreate, nUpdate, nSkip := 0, 0, 0
	for _, item := range p.Items {
		switch item.Action {
		case ActionCreate:
			nCreate++
		case ActionUpdate:
			nUpdate++
		case ActionSkip:
			nSkip++
		}
	}
	fmt.Fprintf(&sb, "\n%d create, %d update, %d skip\n", nCreate, nUpdate, nSkip)
	return sb.String()
}

func flagDisplay(fd FlagDelta) string {
	if fd.Wanted != nil {
		return fd.Wanted.Content
	}
	return fd.Existing.Content
}

func tagDisplay(td TagDelta) string {
	if td.Wanted != nil {
		return td.Wanted.Value
	}
	return td.Existing.Value
}

func hintDisplay(hd HintDelta) string {
	if hd.Wanted != nil {
		return fmt.Sprintf("[%d] %s", hd.Wanted.Cost, hd.Wanted.Content)
	}
	return fmt.Sprintf("[%d] %s", hd.Existing.Cost, hd.Existing.Content)
}
