package instancer

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newInstance(id, team, slug string, expires time.Time) Instance {
	return Instance{
		ID:            id,
		ChallengeSlug: slug,
		TeamID:        team,
		ProjectName:   id,
		Ports:         map[int]int{8000: 30000},
		SpawnedAt:     time.Now().UTC(),
		ExpiresAt:     expires,
	}
}

func TestStore_FreshIsEmpty(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Errorf("fresh store should be empty")
	}
}

func TestStore_PutGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	s, _ := NewStore(path)
	in := newInstance("a-team1-x", "team1", "web/cub3", time.Now().Add(30*time.Minute))
	if err := s.Put(in); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("a-team1-x")
	if !ok {
		t.Fatal("Get missed the instance just put")
	}
	if got.TeamID != "team1" || got.ChallengeSlug != "web/cub3" {
		t.Errorf("round-trip lost fields: %+v", got)
	}
}

func TestStore_RestartLoadsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	s1, _ := NewStore(path)
	_ = s1.Put(newInstance("a-t-1", "t", "x/y", time.Now().Add(time.Hour)))
	_ = s1.Put(newInstance("b-t-2", "t", "x/y", time.Now().Add(time.Hour)))

	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != 2 {
		t.Errorf("reopened store should have 2 entries, got %d", len(s2.List()))
	}
}

func TestStore_DeleteIsIdempotent(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "store.json"))
	_ = s.Put(newInstance("only", "t", "x/y", time.Now().Add(time.Hour)))
	if err := s.Delete("only"); err != nil {
		t.Fatal(err)
	}
	// Delete-after-delete shouldn't error.
	if err := s.Delete("only"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("never-existed"); err != nil {
		t.Fatal(err)
	}
}

func TestStore_Expired(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "store.json"))
	now := time.Now()
	_ = s.Put(newInstance("future", "t", "x/y", now.Add(time.Hour)))
	_ = s.Put(newInstance("past1", "t", "x/y", now.Add(-time.Minute)))
	_ = s.Put(newInstance("past2", "t", "x/y", now.Add(-time.Hour)))

	got := s.Expired(now)
	if len(got) != 2 {
		t.Fatalf("want 2 expired, got %d", len(got))
	}
	if got[0].ID != "past2" || got[1].ID != "past1" {
		t.Errorf("expired should be sorted by ExpiresAt asc: %+v", got)
	}
}

func TestStore_ByTeamAndChallenge(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "store.json"))
	_ = s.Put(newInstance("a", "t1", "web/x", time.Now().Add(time.Hour)))
	_ = s.Put(newInstance("b", "t2", "web/x", time.Now().Add(time.Hour)))

	got, ok := s.ByTeamAndChallenge("t1", "web/x")
	if !ok || got.ID != "a" {
		t.Errorf("lookup t1/web/x = %+v ok=%v", got, ok)
	}
	if _, ok := s.ByTeamAndChallenge("t1", "pwn/y"); ok {
		t.Errorf("lookup for nonexistent should miss")
	}
}

func TestStore_AllocatedHostPorts(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "store.json"))
	a := newInstance("a", "t", "x/y", time.Now().Add(time.Hour))
	a.Ports = map[int]int{8000: 30001}
	b := newInstance("b", "t", "x/y", time.Now().Add(time.Hour))
	b.Ports = map[int]int{8000: 30002, 9000: 30003}
	_ = s.Put(a)
	_ = s.Put(b)

	got := s.AllocatedHostPorts()
	for _, p := range []int{30001, 30002, 30003} {
		if !got[p] {
			t.Errorf("port %d should be allocated", p)
		}
	}
	if got[31000] {
		t.Errorf("unallocated port should not appear")
	}
}

func TestStore_AtomicRewrite(t *testing.T) {
	// Put 50 instances back-to-back; the file should always be valid
	// JSON in between (no half-written state). We check by stat'ing
	// the file size between puts.
	path := filepath.Join(t.TempDir(), "store.json")
	s, _ := NewStore(path)
	for i := 0; i < 50; i++ {
		_ = s.Put(newInstance(string(rune('a'+i)), "t", "x/y", time.Now().Add(time.Hour)))
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat after put %d: %v", i, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("store file is empty after put %d", i)
		}
	}
}

func TestStore_ConcurrentPutsDontRace(t *testing.T) {
	// Run with -race to verify the mutex serializes Put correctly.
	s, _ := NewStore(filepath.Join(t.TempDir(), "store.json"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "x" + string(rune('a'+i))
			_ = s.Put(newInstance(id, "t", "x/y", time.Now().Add(time.Hour)))
		}()
	}
	wg.Wait()
	if len(s.List()) != 20 {
		t.Errorf("want 20 instances after concurrent puts, got %d", len(s.List()))
	}
}
