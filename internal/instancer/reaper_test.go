package instancer

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

func reapStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "i.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func reapChall() *challenge.Challenge {
	c := instancedChall("cub3", "web", 8000)
	c.ChallengeDir = "/tmp"
	return c
}

func TestReaper_TickReapsExpired(t *testing.T) {
	store := reapStore(t)
	runner := NewFakeRunner()
	c := reapChall()

	now := time.Now().UTC()
	_ = store.Put(Instance{ID: "alive", ChallengeSlug: "web/cub3", TeamID: "t1",
		ExpiresAt: now.Add(time.Hour)})
	_ = store.Put(Instance{ID: "dead", ChallengeSlug: "web/cub3", TeamID: "t2",
		ExpiresAt: now.Add(-time.Minute)})

	var log bytes.Buffer
	r := &Reaper{
		Store:   store,
		Runner:  runner,
		ChalIdx: map[string]*challenge.Challenge{"web/cub3": c},
		Logger:  &log,
	}
	r.Tick(context.Background())

	if _, ok := store.Get("dead"); ok {
		t.Errorf("expired instance should be reaped from store")
	}
	if _, ok := store.Get("alive"); !ok {
		t.Errorf("live instance should remain")
	}
	if len(runner.DownCalls) != 1 || runner.DownCalls[0] != "dead" {
		t.Errorf("Down should run once for 'dead', got %v", runner.DownCalls)
	}
	if !strings.Contains(log.String(), "reaped dead") {
		t.Errorf("logger should report reap: %q", log.String())
	}
}

func TestReaper_DownFailsLeavesRecord(t *testing.T) {
	store := reapStore(t)
	runner := NewFakeRunner()
	runner.ErrOnDown = map[string]error{"flaky": errors.New("docker hung")}
	c := reapChall()

	_ = store.Put(Instance{ID: "flaky", ChallengeSlug: "web/cub3", TeamID: "t",
		ExpiresAt: time.Now().Add(-time.Minute)})

	r := &Reaper{
		Store:   store,
		Runner:  runner,
		ChalIdx: map[string]*challenge.Challenge{"web/cub3": c},
	}
	r.Tick(context.Background())

	if _, ok := store.Get("flaky"); !ok {
		t.Errorf("failed Down should leave the record so next tick retries")
	}
}

func TestReaper_OrphanRecordRemoved(t *testing.T) {
	store := reapStore(t)
	runner := NewFakeRunner()

	// Expired instance whose challenge isn't in the catalog (e.g. the
	// author deleted it between deploys). Reaper should still clean
	// up the store record.
	_ = store.Put(Instance{ID: "orphan", ChallengeSlug: "deleted/x", TeamID: "t",
		ExpiresAt: time.Now().Add(-time.Hour)})

	r := &Reaper{Store: store, Runner: runner, ChalIdx: map[string]*challenge.Challenge{}}
	r.Tick(context.Background())

	if _, ok := store.Get("orphan"); ok {
		t.Errorf("orphan should be removed even without a catalog match")
	}
	if len(runner.DownCalls) != 0 {
		t.Errorf("Down should not be called for an orphan: %v", runner.DownCalls)
	}
}

func TestReaper_RunCancelsOnContext(t *testing.T) {
	store := reapStore(t)
	r := &Reaper{
		Store:    store,
		Runner:   NewFakeRunner(),
		ChalIdx:  map[string]*challenge.Challenge{},
		Interval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("reaper did not exit after cancel")
	}
}
