// Package platform defines the interface every CTF platform target
// (CTFd, generic webhook, …) implements, plus the payload helpers they
// share. It imports no adapter; cmd/platform.go picks one from
// platform.type in mctf.config.yml.
package platform

import (
	"context"
	"io"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Platform is one CTF platform target.
type Platform interface {
	// Sync plans and (unless req.DryRun) applies the challenge set. It
	// writes its own human or JSON output to req.Out.
	Sync(ctx context.Context, req SyncRequest) error
	// ResolveUnsync finds what Unsync would remove, without side effects.
	ResolveUnsync(ctx context.Context, category, slug, wantName string) (Target, error)
	Unsync(ctx context.Context, t Target) error
	Configure(ctx context.Context, ev EventConfig) error
}

// SyncRequest carries everything a Sync run needs.
type SyncRequest struct {
	Config         *config.Config
	Challenges     []*challenge.Challenge
	CategoryFilter string
	SlugFilter     string
	// AllowUpdate reconciles challenges that already exist. Platforms
	// without remote state ignore it.
	AllowUpdate bool
	DryRun      bool
	JSON        bool
	Out         io.Writer
}

// Target is a resolved unsync target.
type Target struct {
	Category, Slug string
	// ID is the platform-side id, opaque to callers ("" when the
	// platform has none).
	ID string
	// Description is the one line shown before the y/N prompt.
	Description string
	// Done is the line printed after a successful Unsync.
	Done string
}

// EventConfig is the event-level metadata `mctf platform configure` pushes.
type EventConfig struct {
	Name        string
	Description string
	StartAt     string
	EndAt       string
}
