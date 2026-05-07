package ctfd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func sampleStatic() *challenge.Challenge {
	return &challenge.Challenge{
		Slug: "rsa-baby", CategoryDir: "crypto",
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: "RSA Baby", Author: "alice", Category: "crypto",
				Difficulty: "easy", Wave: 1, Description: "tiny RSA",
				Flags:      []challenge.Flag{{Flag: "mctf{x}", CaseSensitive: true}},
				Tags:       []string{"rsa", "math"},
				NeedDeploy: false,
			},
		},
		Archetype: challenge.ArchetypeStatic,
	}
}

func sampleInstanced() *challenge.Challenge {
	internal := 8000
	return &challenge.Challenge{
		Slug: "cub3", CategoryDir: "web",
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: "CUB3", Author: "carol", Category: "web",
				Difficulty: "medium", Wave: 2,
				Flags:      []challenge.Flag{{Flag: "mctf{c}", CaseSensitive: true}},
				NeedDeploy: true,
			},
			Deploy: &challenge.DeployBlock{
				Subdomain: "cub3", InternalPort: &internal, DynamicInstances: true,
			},
		},
		Archetype: challenge.ArchetypeInstanced,
	}
}

func sampleConfig() *config.Config {
	return &config.Config{
		Version: 1,
		Event: config.Event{
			Slug:   "evt",
			Domain: "ctf.example.com",
			Scoring: &config.Scoring{Type: "dynamic", Initial: 500, Decay: 10, Minimum: 100},
		},
		Instancer: &config.Instancer{URL: "https://instancer.example.com"},
	}
}

func TestBuildPayload_AppliesEventScoring(t *testing.T) {
	// Event default is dynamic; a static-archetype challenge with no
	// per-challenge override picks that up (archetype is independent
	// from CTFd scoring type).
	got := BuildPayload(sampleConfig(), sampleStatic())
	if got.Type != "dynamic" {
		t.Errorf("type = %q", got.Type)
	}
	if got.Initial == nil || *got.Initial != 500 {
		t.Errorf("initial = %v", got.Initial)
	}
	if !strings.Contains(got.Description, "**Author: alice**") {
		t.Errorf("description should append author: %s", got.Description)
	}
	if got.Value != nil {
		t.Errorf("dynamic-scored challenge should not set value: %v", got.Value)
	}
}

func TestBuildPayload_StaticScoringOverride(t *testing.T) {
	c := sampleStatic()
	c.Manifest.Challenge.Scoring = &challenge.Scoring{Type: "static", Points: 250}
	got := BuildPayload(sampleConfig(), c)
	if got.Type != "standard" || got.Value == nil || *got.Value != 250 {
		t.Errorf("override should produce standard/250: type=%q value=%v", got.Type, got.Value)
	}
}

func TestBuildPayload_InstancedIsDynamic(t *testing.T) {
	got := BuildPayload(sampleConfig(), sampleInstanced())
	if got.Type != "dynamic" {
		t.Errorf("type = %q", got.Type)
	}
	if got.Initial == nil || *got.Initial != 500 {
		t.Errorf("initial = %v", got.Initial)
	}
	if got.ConnectionInfo != "https://instancer.example.com" {
		t.Errorf("connection_info = %q", got.ConnectionInfo)
	}
}

func TestPayloadTags_PrependsWaveTag(t *testing.T) {
	got := PayloadTags(sampleStatic())
	if len(got) != 3 || got[0].Value != "wave 1" {
		t.Errorf("tags = %+v", got)
	}
	if got[1].Value != "rsa" || got[2].Value != "math" {
		t.Errorf("explicit tags should follow: %+v", got)
	}
}

func TestPayloadFlags_StampsCaseSensitivity(t *testing.T) {
	got := PayloadFlags(sampleStatic())
	if len(got) != 1 || got[0].Type != "static" || got[0].Data != "case_sensitive" {
		t.Errorf("flag = %+v", got)
	}
}

func TestDiffFlags_AddsMissingDeletesUnwanted(t *testing.T) {
	have := []Flag{
		{ID: 1, Content: "stale", Data: "case_sensitive"},
		{ID: 2, Content: "keepme", Data: "case_sensitive"},
	}
	want := []Flag{
		{Type: "static", Content: "keepme", Data: "case_sensitive"},
		{Type: "static", Content: "newone", Data: "case_sensitive"},
	}
	got := diffFlags(have, want)
	var add, del int
	for _, d := range got {
		switch d.Op {
		case "add":
			add++
			if d.Wanted.Content != "newone" {
				t.Errorf("add wrong: %+v", d.Wanted)
			}
		case "delete":
			del++
			if d.Existing.Content != "stale" {
				t.Errorf("delete wrong: %+v", d.Existing)
			}
		}
	}
	if add != 1 || del != 1 {
		t.Errorf("add=%d delete=%d, want 1/1", add, del)
	}
}

func TestDiff_CreatesWhenMissing(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{})
	defer srv.close()
	c := New(srv.URL, "tok")

	plan, err := Diff(context.Background(), c, sampleConfig(), []*challenge.Challenge{sampleStatic()}, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionCreate {
		t.Errorf("plan = %+v", plan)
	}
}

func TestDiff_SkipsExistingByDefault(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{
		existing: []Challenge{{ID: 7, Name: "RSA Baby", Category: "crypto", Type: "standard"}},
	})
	defer srv.close()
	c := New(srv.URL, "tok")

	plan, err := Diff(context.Background(), c, sampleConfig(), []*challenge.Challenge{sampleStatic()}, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionSkip {
		t.Errorf("expected Skip, got %+v", plan.Items)
	}
	if !strings.Contains(plan.Items[0].Reason, "already exists") {
		t.Errorf("reason = %q", plan.Items[0].Reason)
	}
}

func TestDiff_AllowUpdateProducesUpdate(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{
		existing: []Challenge{{ID: 7, Name: "RSA Baby", Category: "crypto", Type: "standard"}},
		flags:    map[int][]Flag{7: {{ID: 1, Content: "mctf{x}", Data: "case_sensitive"}}},
		tags:     map[int][]Tag{7: {{ID: 1, Value: "wave 1"}, {ID: 2, Value: "rsa"}, {ID: 3, Value: "math"}}},
	})
	defer srv.close()
	c := New(srv.URL, "tok")

	plan, err := Diff(context.Background(), c, sampleConfig(),
		[]*challenge.Challenge{sampleStatic()}, PlanOptions{AllowUpdate: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Action != ActionUpdate {
		t.Errorf("want Update, got %s", plan.Items[0].Action)
	}
	// All flags + tags already match → no deltas.
	if len(plan.Items[0].Flags) != 0 || len(plan.Items[0].Tags) != 0 {
		t.Errorf("expected no deltas when fully in sync, got flags=%d tags=%d",
			len(plan.Items[0].Flags), len(plan.Items[0].Tags))
	}
}

func TestApply_CreatesChallengeWithChildResources(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{})
	defer srv.close()
	c := New(srv.URL, "tok")

	plan, err := Diff(context.Background(), c, sampleConfig(),
		[]*challenge.Challenge{sampleStatic()}, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := Apply(context.Background(), c, plan)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Errorf("applied = %d", applied)
	}

	st := srv.snapshot()
	if len(st.existing) != 1 {
		t.Fatalf("expected 1 created challenge, got %d", len(st.existing))
	}
	if len(st.flags[st.existing[0].ID]) != 1 {
		t.Errorf("expected 1 flag created, got %d", len(st.flags[st.existing[0].ID]))
	}
	if len(st.tags[st.existing[0].ID]) != 3 {
		t.Errorf("expected 3 tags (wave + 2 explicit), got %d", len(st.tags[st.existing[0].ID]))
	}
}

func TestRender_HasActionLines(t *testing.T) {
	plan := &Plan{Items: []Item{
		{Slug: "web/x", Action: ActionCreate, Reason: "missing"},
		{Slug: "web/y", Action: ActionSkip, Reason: "exists"},
	}}
	out := plan.Render()
	for _, want := range []string{"create", "web/x", "skip", "1 create, 0 update, 1 skip"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
}

// --- fake CTFd server ---

type syncFakeState struct {
	existing []Challenge
	flags    map[int][]Flag
	tags     map[int][]Tag
	hints    map[int][]Hint
	nextID   int32
}

type syncFake struct {
	*httptest.Server
	state *syncFakeState
}

func (s *syncFake) close() { s.Server.Close() }

func (s *syncFake) snapshot() syncFakeState {
	// Returned by value; sufficient for read-after-write inspection.
	return *s.state
}

func newSyncFake(t *testing.T, initial syncFakeState) *syncFake {
	t.Helper()
	state := &initial
	if state.flags == nil {
		state.flags = map[int][]Flag{}
	}
	if state.tags == nil {
		state.tags = map[int][]Tag{}
	}
	if state.hints == nil {
		state.hints = map[int][]Hint{}
	}
	if state.nextID == 0 {
		state.nextID = 100
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		path := r.URL.Path
		method := r.Method

		switch {
		case method == "GET" && path == "/api/v1/challenges":
			writeEnvelope(w, state.existing)
		case method == "POST" && path == "/api/v1/challenges":
			var in Challenge
			_ = json.Unmarshal(body, &in)
			in.ID = int(atomic.AddInt32(&state.nextID, 1))
			state.existing = append(state.existing, in)
			writeEnvelope(w, in)
		case method == "PATCH" && strings.HasPrefix(path, "/api/v1/challenges/"):
			id := pathID(path)
			for i := range state.existing {
				if state.existing[i].ID == id {
					var patch Challenge
					_ = json.Unmarshal(body, &patch)
					state.existing[i].Name = patch.Name
					state.existing[i].Category = patch.Category
					state.existing[i].Description = patch.Description
					writeEnvelope(w, state.existing[i])
					return
				}
			}
			http.Error(w, "not found", 404)
		case method == "GET" && strings.HasSuffix(path, "/flags"):
			id := pathID(strings.TrimSuffix(path, "/flags"))
			writeEnvelope(w, state.flags[id])
		case method == "POST" && path == "/api/v1/flags":
			var in Flag
			_ = json.Unmarshal(body, &in)
			in.ID = int(atomic.AddInt32(&state.nextID, 1))
			state.flags[in.ChallengeID] = append(state.flags[in.ChallengeID], in)
			writeEnvelope(w, in)
		case method == "GET" && strings.HasSuffix(path, "/tags"):
			id := pathID(strings.TrimSuffix(path, "/tags"))
			writeEnvelope(w, state.tags[id])
		case method == "POST" && path == "/api/v1/tags":
			var in Tag
			_ = json.Unmarshal(body, &in)
			in.ID = int(atomic.AddInt32(&state.nextID, 1))
			state.tags[in.ChallengeID] = append(state.tags[in.ChallengeID], in)
			writeEnvelope(w, in)
		case method == "GET" && strings.HasSuffix(path, "/hints"):
			id := pathID(strings.TrimSuffix(path, "/hints"))
			writeEnvelope(w, state.hints[id])
		case method == "POST" && path == "/api/v1/hints":
			var in Hint
			_ = json.Unmarshal(body, &in)
			in.ID = int(atomic.AddInt32(&state.nextID, 1))
			state.hints[in.ChallengeID] = append(state.hints[in.ChallengeID], in)
			writeEnvelope(w, in)
		default:
			t.Logf("fake: unhandled %s %s", method, path)
			writeEnvelope(w, map[string]any{})
		}
	}))
	return &syncFake{Server: srv, state: state}
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{"success": true, "data": data})
	_, _ = w.Write(body)
}

func pathID(path string) int {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return 0
	}
	var id int
	_, _ = fmt.Sscanf(path[i+1:], "%d", &id)
	return id
}
