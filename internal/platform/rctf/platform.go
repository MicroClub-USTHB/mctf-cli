package rctf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

// Platform adapts the rCTF client to the platform.Platform interface.
type Platform struct {
	client *Client
}

// NewPlatform returns an rCTF platform target.
func NewPlatform(baseURL, token string) (*Platform, error) {
	c, err := NewClient(baseURL, token)
	if err != nil {
		return nil, err
	}
	return &Platform{client: c}, nil
}

// Action represents a sync action for one challenge.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
)

// PlanItem represents one planned challenge sync operation.
type PlanItem struct {
	Slug     string    // <category>/<slug>
	ID       string    // rCTF challenge ID
	Action   Action    // create, update, skip
	Reason   string    // explanation for action
	Payload  Challenge // payload to submit to rCTF
	Existing *Challenge
}

// PlanItemJSON is the JSON representation emitted when --json is passed.
type PlanItemJSON struct {
	Action   string `json:"action"`
	Slug     string `json:"slug"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// Sync compares local challenges with remote rCTF challenges, renders
// the plan (human-readable or JSON), and applies changes if not dry-run.
func (p *Platform) Sync(ctx context.Context, req platform.SyncRequest) error {
	challs := filterChallenges(req.Challenges, req.CategoryFilter, req.SlugFilter)
	if len(challs) == 0 && !req.DryRun {
		return errors.New("no challenges matched the filters — nothing to send")
	}

	remoteList, err := p.client.ListChallenges(ctx)
	if err != nil {
		return fmt.Errorf("fetch rCTF challenges: %w", err)
	}

	remoteMap := make(map[string]Challenge, len(remoteList))
	for _, r := range remoteList {
		remoteMap[r.ID] = r
	}

	plan := p.diff(req.Config, challs, remoteMap, req.AllowUpdate)

	if req.JSON {
		if err := printPlanJSON(req.Out, plan); err != nil {
			return err
		}
	} else {
		printPlanHuman(req.Out, plan)
	}

	if req.DryRun {
		return nil
	}

	applied := 0
	for _, item := range plan {
		if item.Action == ActionSkip {
			continue
		}
		if _, err := p.client.PutChallenge(ctx, item.ID, item.Payload); err != nil {
			return fmt.Errorf("sync %s: %w", item.Slug, err)
		}
		applied++
	}

	if !req.JSON {
		fmt.Fprintf(req.Out, "\napplied %d action(s)\n", applied)
	}
	return nil
}

// ResolveUnsync finds the rCTF challenge matching the given category and slug.
func (p *Platform) ResolveUnsync(ctx context.Context, category, slug, wantName string) (platform.Target, error) {
	existing, err := p.client.ListChallenges(ctx)
	if err != nil {
		return platform.Target{}, fmt.Errorf("list challenges: %w", err)
	}

	var match *Challenge
	candidateID := category + "-" + slug

	for i := range existing {
		c := &existing[i]
		if c.ID == candidateID || c.ID == slug {
			match = c
			break
		}
		if strings.EqualFold(c.Category, category) && (strings.EqualFold(c.Name, wantName) || strings.EqualFold(c.ID, slug)) {
			match = c
			break
		}
	}

	if match == nil {
		return platform.Target{}, fmt.Errorf("no rCTF challenge matched %s/%s (looked for category=%s, name=%q)",
			category, slug, category, wantName)
	}

	return platform.Target{
		Category:    category,
		Slug:        slug,
		ID:          match.ID,
		Description: fmt.Sprintf("delete rCTF challenge id=%q name=%q category=%q", match.ID, match.Name, match.Category),
		Done:        fmt.Sprintf("deleted id=%q", match.ID),
	}, nil
}

// Unsync deletes a challenge on the rCTF instance.
func (p *Platform) Unsync(ctx context.Context, t platform.Target) error {
	if t.ID == "" {
		return errors.New("empty rCTF challenge id")
	}
	return p.client.DeleteChallenge(ctx, t.ID)
}

// Configure pushes event metadata to rCTF.
func (p *Platform) Configure(ctx context.Context, ev platform.EventConfig) error {
	meta := ConfigMeta{
		Name:        ev.Name,
		Description: ev.Description,
	}

	if ev.StartAt != "" {
		if t, err := time.Parse(time.RFC3339, ev.StartAt); err == nil {
			ms := t.UnixMilli()
			meta.StartTime = &ms
		}
	}
	if ev.EndAt != "" {
		if t, err := time.Parse(time.RFC3339, ev.EndAt); err == nil {
			ms := t.UnixMilli()
			meta.EndTime = &ms
		}
	}

	return p.client.Configure(ctx, meta)
}

func (p *Platform) diff(cfg *config.Config, challs []*challenge.Challenge, remote map[string]Challenge, allowUpdate bool) []PlanItem {
	items := make([]PlanItem, 0, len(challs))

	for _, c := range challs {
		slug := c.CategoryDir + "/" + c.Slug
		id := c.CategoryDir + "-" + c.Slug
		payload := buildChallengePayload(cfg, c, id)

		existing, found := remote[id]
		if !found {
			// Try fallback matching by bare slug if remote was created that way
			existing, found = remote[c.Slug]
			if found {
				id = c.Slug
				payload.ID = id
			}
		}

		if !found {
			items = append(items, PlanItem{
				Slug:    slug,
				ID:      id,
				Action:  ActionCreate,
				Reason:  "new challenge",
				Payload: payload,
			})
			continue
		}

		if challengesEqual(payload, existing) {
			if allowUpdate {
				items = append(items, PlanItem{
					Slug:     slug,
					ID:       id,
					Action:   ActionUpdate,
					Reason:   "forced update (--allow-update)",
					Payload:  payload,
					Existing: &existing,
				})
			} else {
				items = append(items, PlanItem{
					Slug:     slug,
					ID:       id,
					Action:   ActionSkip,
					Reason:   "up to date (use --allow-update to force)",
					Payload:  payload,
					Existing: &existing,
				})
			}
			continue
		}

		items = append(items, PlanItem{
			Slug:     slug,
			ID:       id,
			Action:   ActionUpdate,
			Reason:   "metadata changed",
			Payload:  payload,
			Existing: &existing,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Slug < items[j].Slug
	})

	return items
}

func buildChallengePayload(cfg *config.Config, c *challenge.Challenge, id string) Challenge {
	desc := platform.DescriptionWithAuthor(c.Manifest.Challenge.Description, c.Manifest.Challenge.Author)
	if conn := platform.ConnectionInfo(cfg, c); conn != "" {
		desc += "\n\n**Connection:** `" + conn + "`"
	}

	cat := c.Manifest.Challenge.Category
	if cat == "" {
		cat = c.CategoryDir
	}

	s := platform.ResolveScoring(cfg, c.Manifest.Challenge.Scoring)

	var flags []ChallengeFlag
	for _, f := range c.Manifest.Challenge.Flags {
		if f.Flag != "" {
			flags = append(flags, ChallengeFlag{Flag: f.Flag})
		}
	}

	chall := Challenge{
		ID:               id,
		Name:             c.Manifest.Challenge.Name,
		Author:           c.Manifest.Challenge.Author,
		Description:      desc,
		Category:         cat,
		TiebreakEligible: true,
		Flags:            flags,
	}

	if s.Type == "dynamic" {
		chall.Initial = s.Initial
		chall.Min = s.Minimum
		chall.Decay = s.Decay
	} else {
		chall.Initial = s.Points
		chall.Min = s.Points
		chall.Decay = 0
	}

	return chall
}

func challengesEqual(a, b Challenge) bool {
	if a.Name != b.Name ||
		a.Description != b.Description ||
		a.Category != b.Category ||
		a.Author != b.Author ||
		a.TiebreakEligible != b.TiebreakEligible ||
		a.Initial != b.Initial ||
		a.Min != b.Min ||
		a.Decay != b.Decay {
		return false
	}

	if len(a.Flags) != len(b.Flags) {
		return false
	}
	for i := range a.Flags {
		if a.Flags[i].Flag != b.Flags[i].Flag {
			return false
		}
	}

	if len(a.Files) != len(b.Files) {
		return false
	}
	return reflect.DeepEqual(a.Files, b.Files)
}

func filterChallenges(challs []*challenge.Challenge, catFilter, slugFilter string) []*challenge.Challenge {
	var out []*challenge.Challenge
	for _, c := range challs {
		if catFilter != "" && c.CategoryDir != catFilter {
			continue
		}
		if slugFilter != "" && c.Slug != slugFilter {
			continue
		}
		out = append(out, c)
	}
	return out
}

func printPlanHuman(out any, items []PlanItem) {
	w, ok := out.(interface{ Write([]byte) (int, error) })
	if !ok {
		return
	}

	fmt.Fprintln(w, "Planned rCTF sync:")
	for _, it := range items {
		fmt.Fprintf(w, "  %-6s  %-24s  (%s)\n", it.Action, it.Slug, it.Reason)
	}
}

func printPlanJSON(out any, items []PlanItem) error {
	w, ok := out.(interface{ Write([]byte) (int, error) })
	if !ok {
		return nil
	}

	rows := make([]PlanItemJSON, 0, len(items))
	for _, it := range items {
		rows = append(rows, PlanItemJSON{
			Action:   string(it.Action),
			Slug:     it.Slug,
			ID:       it.ID,
			Name:     it.Payload.Name,
			Category: it.Payload.Category,
			Reason:   it.Reason,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}
