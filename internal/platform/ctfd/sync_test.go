package ctfd

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			Slug:    "evt",
			Domain:  "ctf.example.com",
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

func TestPayloadFiles_EnumeratesAssets(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"handout.zip", "image.png", ".gitkeep", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(assets, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := sampleStatic()
	c.AssetsDir = assets
	got := PayloadFiles(c)
	if len(got) != 2 {
		t.Fatalf("want 2 assets (hidden filtered), got %d: %+v", len(got), got)
	}
	if got[0].WantedName != "handout.zip" || got[1].WantedName != "image.png" {
		t.Errorf("not sorted alphabetically: %+v", got)
	}
	for _, fd := range got {
		if fd.Op != "add" || fd.WantedPath == "" {
			t.Errorf("bad delta: %+v", fd)
		}
	}
}

func TestDiffFiles_ByBasename(t *testing.T) {
	have := []File{
		{ID: 1, Location: "uploads/abc/keep.zip", ChallengeID: 5},
		{ID: 2, Location: "uploads/def/stale.zip", ChallengeID: 5},
	}
	want := []FileDelta{
		{Op: "add", WantedName: "keep.zip", WantedPath: "/local/keep.zip"},
		{Op: "add", WantedName: "new.zip", WantedPath: "/local/new.zip"},
	}
	got := diffFiles(have, want)
	var add, del int
	for _, d := range got {
		switch d.Op {
		case "add":
			add++
			if d.WantedName != "new.zip" {
				t.Errorf("unexpected add: %+v", d)
			}
		case "delete":
			del++
			if d.Existing.ID != 2 {
				t.Errorf("unexpected delete: %+v", d)
			}
		}
	}
	if add != 1 || del != 1 {
		t.Errorf("add=%d delete=%d, want 1/1", add, del)
	}
}

func TestDiffFiles_ReuploadsOnContentChange(t *testing.T) {
	have := []File{
		{ID: 7, Location: "uploads/abc/handout.zip", SHA1: "oldhash", ChallengeID: 5},
	}
	want := []FileDelta{
		{Op: "add", WantedName: "handout.zip", WantedPath: "/local/handout.zip", WantedSHA1: "newhash"},
	}
	got := diffFiles(have, want)
	if len(got) != 2 {
		t.Fatalf("want delete+add for the changed file, got %d: %+v", len(got), got)
	}
	// add sorts before delete for the same name, so the new copy is
	// uploaded before the stale one is removed.
	if got[0].Op != "add" || got[0].WantedSHA1 != "newhash" {
		t.Errorf("first delta should add the local file: %+v", got[0])
	}
	if got[1].Op != "delete" || got[1].Existing.ID != 7 {
		t.Errorf("second delta should delete stale id=7: %+v", got[1])
	}
}

func TestDiffFiles_SkipsWhenContentSame(t *testing.T) {
	have := []File{{ID: 7, Location: "uploads/abc/handout.zip", SHA1: "samehash", ChallengeID: 5}}
	want := []FileDelta{{Op: "add", WantedName: "handout.zip", WantedSHA1: "samehash"}}
	if got := diffFiles(have, want); len(got) != 0 {
		t.Errorf("identical content should produce no deltas, got %+v", got)
	}
}

func TestDiffFiles_SkipsWhenHashUnavailable(t *testing.T) {
	// Older CTFd omits sha1sum: fall back to basename-only (no churn).
	have := []File{{ID: 7, Location: "uploads/abc/handout.zip", ChallengeID: 5}}
	want := []FileDelta{{Op: "add", WantedName: "handout.zip", WantedSHA1: "newhash"}}
	if got := diffFiles(have, want); len(got) != 0 {
		t.Errorf("unknown remote hash should not trigger re-upload, got %+v", got)
	}
}

func TestPayloadFiles_ComputesSHA1(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("handout-bytes")
	if err := os.WriteFile(filepath.Join(assets, "handout.zip"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(content)
	want := hex.EncodeToString(sum[:])

	c := sampleStatic()
	c.AssetsDir = assets
	got := PayloadFiles(c)
	if len(got) != 1 {
		t.Fatalf("want 1 asset, got %d", len(got))
	}
	if got[0].WantedSHA1 != want {
		t.Errorf("WantedSHA1 = %q, want %q", got[0].WantedSHA1, want)
	}
}

func TestApply_UploadsAssets(t *testing.T) {
	srv := newSyncFake(t, syncFakeState{})
	defer srv.close()
	c := New(srv.URL, "tok")

	// Set up a challenge with a real assets directory.
	dir := t.TempDir()
	chRoot := filepath.Join(dir, "crypto", "rsa-baby")
	assets := filepath.Join(chRoot, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "transcript.txt"), []byte("...rsa transcript..."), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := sampleStatic()
	ch.AssetsDir = assets

	plan, err := Diff(context.Background(), c, sampleConfig(), []*challenge.Challenge{ch}, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items[0].Files) != 1 || plan.Items[0].Files[0].WantedName != "transcript.txt" {
		t.Fatalf("plan should include transcript.txt: %+v", plan.Items[0].Files)
	}

	if _, err := Apply(context.Background(), c, plan); err != nil {
		t.Fatal(err)
	}

	st := srv.snapshot()
	if len(st.existing) != 1 {
		t.Fatalf("want 1 challenge created, got %d", len(st.existing))
	}
	cid := st.existing[0].ID
	if len(st.files[cid]) != 1 {
		t.Fatalf("want 1 file uploaded, got %d", len(st.files[cid]))
	}
	if st.files[cid][0].Basename() != "transcript.txt" {
		t.Errorf("uploaded basename = %q", st.files[cid][0].Basename())
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
	files    map[int][]File
	nextID   int32
}

type syncFake struct {
	*httptest.Server
	state *syncFakeState
}

func (s *syncFake) close() { s.Close() }

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
	if state.files == nil {
		state.files = map[int][]File{}
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
		case method == "GET" && path == "/api/v1/files":
			// CTFd returns all files; the client filters by challenge.
			var all []File
			for _, fs := range state.files {
				all = append(all, fs...)
			}
			writeEnvelope(w, all)
		case method == "POST" && path == "/api/v1/files":
			// Multipart upload. Parse form and stash one File per request.
			r2 := r
			r2.Body = io.NopCloser(strings.NewReader(string(body)))
			if err := r2.ParseMultipartForm(1 << 20); err != nil {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"success": false, "errors": "parse"}`))
				return
			}
			var cid int
			_, _ = fmt.Sscanf(r2.FormValue("challenge_id"), "%d", &cid)
			fhs := r2.MultipartForm.File["file"]
			if len(fhs) == 0 {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"success": false, "errors": "no file"}`))
				return
			}
			f := File{
				ID:          int(atomic.AddInt32(&state.nextID, 1)),
				Type:        "challenge",
				Location:    fmt.Sprintf("uploads/%d/%s", state.nextID, fhs[0].Filename),
				ChallengeID: cid,
			}
			state.files[cid] = append(state.files[cid], f)
			writeEnvelope(w, []File{f})
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
