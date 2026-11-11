package ctfd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

// Platform adapts the CTFd client and sync planner to platform.Platform.
type Platform struct {
	client *Client
}

// NewPlatform returns a CTFd platform target for baseURL authenticated
// with token.
func NewPlatform(baseURL, token string) *Platform {
	return &Platform{client: New(baseURL, token)}
}

// Sync diffs the local challenges against CTFd, prints the plan, and
// applies it unless req.DryRun is set.
func (p *Platform) Sync(ctx context.Context, req platform.SyncRequest) error {
	plan, err := Diff(ctx, p.client, req.Config, req.Challenges, PlanOptions{
		CategoryFilter: req.CategoryFilter,
		SlugFilter:     req.SlugFilter,
		AllowUpdate:    req.AllowUpdate,
	})
	if err != nil {
		return err
	}

	if req.JSON {
		if err := PrintPlanJSON(req.Out, plan); err != nil {
			return err
		}
	} else {
		fmt.Fprint(req.Out, plan.Render())
	}

	if req.DryRun {
		return nil
	}

	applied, err := Apply(ctx, p.client, plan)
	if err != nil {
		return err
	}
	if !req.JSON {
		fmt.Fprintf(req.Out, "\napplied %d action(s)\n", applied)
	}
	return nil
}

// ResolveUnsync looks up the CTFd challenge matching category/slug.
func (p *Platform) ResolveUnsync(ctx context.Context, category, slug, wantName string) (platform.Target, error) {
	existing, err := p.client.ListChallenges(ctx)
	if err != nil {
		return platform.Target{}, fmt.Errorf("list challenges: %w", err)
	}
	target := FindUnsyncTarget(existing, category, slug, wantName)
	if target == nil {
		return platform.Target{}, fmt.Errorf("no CTFd challenge matched %s/%s (looked for category=%s, name=%q)",
			category, slug, category, wantName)
	}
	return platform.Target{
		Category: category,
		Slug:     slug,
		ID:       strconv.Itoa(target.ID),
		Description: fmt.Sprintf("delete CTFd challenge id=%d category=%q name=%q",
			target.ID, target.Category, target.Name),
		Done: fmt.Sprintf("deleted id=%d", target.ID),
	}, nil
}

// Unsync deletes the resolved challenge. CTFd cascades the delete to
// flags, tags, hints, and files.
func (p *Platform) Unsync(ctx context.Context, t platform.Target) error {
	id, err := strconv.Atoi(t.ID)
	if err != nil {
		return fmt.Errorf("invalid CTFd challenge id %q", t.ID)
	}
	if err := p.client.DeleteChallenge(ctx, id); err != nil {
		return fmt.Errorf("delete challenge id=%d: %w", id, err)
	}
	return nil
}

// Configure pushes event-level settings to /api/v1/configs.
func (p *Platform) Configure(ctx context.Context, ev platform.EventConfig) error {
	return p.client.Configure(ctx, EventConfig{
		Name:        ev.Name,
		Description: ev.Description,
		StartAt:     ev.StartAt,
		EndAt:       ev.EndAt,
	})
}

// FindUnsyncTarget picks one CTFd challenge from `existing` that
// corresponds to <category>/<slug>. It tries:
//
//  1. category + exact name match (case-insensitive) using `wantName`
//     from the local manifest, if provided.
//  2. category + slug-derived name match (the fallback when the local
//     challenge has already been removed).
//
// Returns nil if no candidate is found.
func FindUnsyncTarget(existing []Challenge, category, slug, wantName string) *Challenge {
	for i := range existing {
		ec := &existing[i]
		if !strings.EqualFold(ec.Category, category) {
			continue
		}
		if wantName != "" && strings.EqualFold(ec.Name, wantName) {
			return ec
		}
	}
	// Fallback: slug-from-name match.
	for i := range existing {
		ec := &existing[i]
		if !strings.EqualFold(ec.Category, category) {
			continue
		}
		if challenge.SlugFromName(ec.Name) == slug {
			return ec
		}
	}
	return nil
}

type planRow struct {
	Slug       string    `json:"slug"`
	Action     Action    `json:"action"`
	Reason     string    `json:"reason"`
	ExistingID int       `json:"existing_id,omitempty"`
	Payload    Challenge `json:"payload"`
}

// PrintPlanJSON writes the plan as an indented JSON array of rows.
func PrintPlanJSON(w io.Writer, plan *Plan) error {
	rows := make([]planRow, 0, len(plan.Items))
	for _, it := range plan.Items {
		rows = append(rows, planRow{
			Slug:       it.Slug,
			Action:     it.Action,
			Reason:     it.Reason,
			ExistingID: it.ExistingID,
			Payload:    it.Payload,
		})
	}
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(body))
	return err
}
