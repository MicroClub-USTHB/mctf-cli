package instancer

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

// Reaper tears down expired instances on a regular interval.
//
// It is safe to run alongside the HTTP server: both share the store
// (which is mutex-serialized) and the runner. The reaper holds no
// state of its own.
type Reaper struct {
	Store    *Store
	Runner   Runner
	ChalIdx  map[string]*challenge.Challenge // <cat>/<slug> → challenge
	Interval time.Duration
	// Logger gets one line per reap action. nil disables logging.
	Logger io.Writer
}

// Run blocks until ctx is canceled, ticking every Interval. Returns
// ctx.Err() on shutdown.
func (r *Reaper) Run(ctx context.Context) error {
	if r.Interval <= 0 {
		r.Interval = 30 * time.Second
	}
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	// One immediate tick on startup so a daemon that just rebooted
	// doesn't wait Interval before cleaning up stale instances.
	r.Tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			r.Tick(ctx)
		}
	}
}

// Tick performs one reap pass. Exposed for tests so they don't have
// to wait for the ticker.
func (r *Reaper) Tick(ctx context.Context) {
	expired := r.Store.Expired(time.Now().UTC())
	for _, inst := range expired {
		if ctx.Err() != nil {
			return
		}
		c, ok := r.ChalIdx[inst.ChallengeSlug]
		if !ok {
			r.logf("reaper: %s — challenge no longer in catalog, removing record\n", inst.ID)
			_ = r.Store.Delete(inst.ID)
			continue
		}
		spec := SpawnSpec{ID: inst.ID, ComposeFile: c.ChallengeDir + "/docker-compose.yml"}
		downCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := r.Runner.Down(downCtx, spec, false)
		cancel()
		if err != nil {
			r.logf("reaper: %s — down failed: %v\n", inst.ID, err)
			// Leave the record in the store so the next tick retries
			// (or an operator can run `mctf instance destroy --force`).
			continue
		}
		if err := r.Store.Delete(inst.ID); err != nil {
			r.logf("reaper: %s — store delete failed: %v\n", inst.ID, err)
			continue
		}
		r.logf("reaper: reaped %s (team=%s)\n", inst.ID, inst.TeamID)
	}
}

func (r *Reaper) logf(format string, args ...any) {
	if r.Logger == nil {
		return
	}
	fmt.Fprintf(r.Logger, format, args...)
}
