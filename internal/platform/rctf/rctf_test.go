package rctf

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/platform"
)

func TestNewClient_Validation(t *testing.T) {
	cases := []struct {
		url     string
		token   string
		wantErr bool
	}{
		{"https://rctf.example.com", "tok", false},
		{"http://localhost:8000", "tok", false},
		{"", "tok", true},
		{"not-a-url", "tok", true},
		{"ftp://rctf.example.com", "tok", true},
	}

	for _, tc := range cases {
		_, err := NewClient(tc.url, tc.token)
		if (err != nil) != tc.wantErr {
			t.Errorf("NewClient(%q) err = %v, wantErr = %v", tc.url, err, tc.wantErr)
		}
	}
}

func TestClient_AuthHeaderAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"kind":    "badToken",
				"message": "Token is invalid.",
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Response[[]Challenge]{
			Kind: "goodChallenges",
			Data: []Challenge{},
		})
	}))
	defer srv.Close()

	// Bad token
	badClient, err := NewClient(srv.URL, "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = badClient.ListChallenges(context.Background())
	if err == nil || !strings.Contains(err.Error(), "badToken") {
		t.Fatalf("expected badToken error, got: %v", err)
	}

	// Good token
	goodClient, err := NewClient(srv.URL, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	challs, err := goodClient.ListChallenges(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(challs) != 0 {
		t.Errorf("expected 0 challenges, got %d", len(challs))
	}
}

func TestClient_CRUD(t *testing.T) {
	var stored []Challenge

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/challs":
			_ = json.NewEncoder(w).Encode(Response[[]Challenge]{
				Kind: "goodChallenges",
				Data: stored,
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/challs/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/challs/")
			var c Challenge
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			c.ID = id
			// replace or append
			updated := false
			for i := range stored {
				if stored[i].ID == id {
					stored[i] = c
					updated = true
					break
				}
			}
			if !updated {
				stored = append(stored, c)
			}
			_ = json.NewEncoder(w).Encode(Response[Challenge]{
				Kind: "goodChallenge",
				Data: c,
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/challs/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/challs/")
			newStored := stored[:0]
			found := false
			for _, c := range stored {
				if c.ID == id {
					found = true
				} else {
					newStored = append(newStored, c)
				}
			}
			stored = newStored
			if !found {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(Response[any]{Kind: "badChallenge", Message: "Not found"})
				return
			}
			_ = json.NewEncoder(w).Encode(Response[any]{Kind: "goodChallenge"})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/admin/config":
			var update AdminConfigUpdate
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(Response[any]{Kind: "goodConfig"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-tok")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 1. Put challenge
	created, err := client.PutChallenge(ctx, "web-test", Challenge{
		Name:        "Test Web",
		Description: "A web challenge",
		Category:    "web",
		Initial:     100,
	})
	if err != nil {
		t.Fatalf("PutChallenge failed: %v", err)
	}
	if created.ID != "web-test" || created.Name != "Test Web" {
		t.Errorf("created = %+v", created)
	}

	// 2. List challenges
	list, err := client.ListChallenges(ctx)
	if err != nil {
		t.Fatalf("ListChallenges failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != "web-test" {
		t.Errorf("list = %+v", list)
	}

	// 3. Configure
	nowMs := time.Now().UnixMilli()
	if err := client.Configure(ctx, ConfigMeta{Name: "CTF 2026", StartTime: &nowMs}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	// 4. Delete challenge
	if err := client.DeleteChallenge(ctx, "web-test"); err != nil {
		t.Fatalf("DeleteChallenge failed: %v", err)
	}

	// 5. Delete not found
	if err := client.DeleteChallenge(ctx, "web-test"); err == nil {
		t.Error("expected error deleting non-existent challenge, got nil")
	}
}

func sampleChall(category, slug, name, desc string, points int, flags ...string) *challenge.Challenge {
	var flagList []challenge.Flag
	for _, f := range flags {
		flagList = append(flagList, challenge.Flag{Flag: f})
	}
	p := points
	return &challenge.Challenge{
		CategoryDir: category,
		Slug:        slug,
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{
				Name:        name,
				Category:    category,
				Description: desc,
				Author:      "Author",
				Scoring: &challenge.Scoring{
					Type:   "static",
					Points: p,
				},
				Flags: flagList,
			},
		},
	}
}

func TestPlatform_Sync_CreateUpdateSkip(t *testing.T) {
	var remoteCalls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls = append(remoteCalls, r.Method+" "+r.URL.Path)

		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/challs" {
			// Initially remote has web-pasteur up to date, and pwn-bof with old description
			pasteur := buildChallengePayload(nil, sampleChall("web", "pasteur", "Pasteur", "old web", 100, "flag{pasteur}"), "web-pasteur")
			bofOld := buildChallengePayload(nil, sampleChall("pwn", "bof", "BOF", "initial bof", 100, "flag{old}"), "pwn-bof")

			_ = json.NewEncoder(w).Encode(Response[[]Challenge]{
				Kind: "goodChallenges",
				Data: []Challenge{pasteur, bofOld},
			})
			return
		}

		if r.Method == http.MethodPut {
			var c Challenge
			_ = json.NewDecoder(r.Body).Decode(&c)
			_ = json.NewEncoder(w).Encode(Response[Challenge]{
				Kind: "goodChallenge",
				Data: c,
			})
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plat, err := NewPlatform(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	challs := []*challenge.Challenge{
		sampleChall("web", "pasteur", "Pasteur", "old web", 100, "flag{pasteur}"), // unchanged -> skip
		sampleChall("pwn", "bof", "BOF", "updated bof", 100, "flag{new}"),         // changed -> update
		sampleChall("crypto", "rsa", "RSA", "brand new rsa", 200, "flag{rsa}"),    // absent -> create
	}

	var buf bytes.Buffer
	req := platform.SyncRequest{
		Config:      &config.Config{Event: config.Event{Slug: "test-event"}},
		Challenges:  challs,
		AllowUpdate: false,
		DryRun:      false,
		Out:         &buf,
	}

	ctx := context.Background()
	if err := plat.Sync(ctx, req); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "skip    web/pasteur") {
		t.Errorf("output should skip web/pasteur:\n%s", output)
	}
	if !strings.Contains(output, "update  pwn/bof") {
		t.Errorf("output should update pwn/bof:\n%s", output)
	}
	if !strings.Contains(output, "create  crypto/rsa") {
		t.Errorf("output should create crypto/rsa:\n%s", output)
	}
	if !strings.Contains(output, "applied 2 action(s)") {
		t.Errorf("output should report 2 applied actions:\n%s", output)
	}
}

func TestPlatform_Sync_DryRun(t *testing.T) {
	putCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(Response[[]Challenge]{
				Kind: "goodChallenges",
				Data: []Challenge{},
			})
			return
		}
		if r.Method == http.MethodPut {
			putCount++
		}
	}))
	defer srv.Close()

	plat, err := NewPlatform(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	challs := []*challenge.Challenge{
		sampleChall("web", "pasteur", "Pasteur", "desc", 100, "flag{1}"),
	}

	var buf bytes.Buffer
	req := platform.SyncRequest{
		Config:     &config.Config{Event: config.Event{Slug: "test-event"}},
		Challenges: challs,
		DryRun:     true,
		Out:        &buf,
	}

	if err := plat.Sync(context.Background(), req); err != nil {
		t.Fatalf("Sync dry-run failed: %v", err)
	}

	if putCount != 0 {
		t.Errorf("expected 0 PUT requests in dry run, got %d", putCount)
	}
	if !strings.Contains(buf.String(), "create  web/pasteur") {
		t.Errorf("expected plan output in dry run, got:\n%s", buf.String())
	}
}

func TestPlatform_Sync_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response[[]Challenge]{
			Kind: "goodChallenges",
			Data: []Challenge{},
		})
	}))
	defer srv.Close()

	plat, err := NewPlatform(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	challs := []*challenge.Challenge{
		sampleChall("web", "pasteur", "Pasteur", "desc", 100, "flag{1}"),
	}

	var buf bytes.Buffer
	req := platform.SyncRequest{
		Config:     &config.Config{Event: config.Event{Slug: "test-event"}},
		Challenges: challs,
		DryRun:     true,
		JSON:       true,
		Out:        &buf,
	}

	if err := plat.Sync(context.Background(), req); err != nil {
		t.Fatalf("Sync JSON failed: %v", err)
	}

	var items []PlanItemJSON
	if err := json.Unmarshal(buf.Bytes(), &items); err != nil {
		t.Fatalf("failed to decode JSON output: %v\nOutput: %s", err, buf.String())
	}
	if len(items) != 1 || items[0].Action != "create" || items[0].Slug != "web/pasteur" {
		t.Errorf("unexpected items: %+v", items)
	}
}

func TestPlatform_Unsync(t *testing.T) {
	deletedID := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/challs" {
			_ = json.NewEncoder(w).Encode(Response[[]Challenge]{
				Kind: "goodChallenges",
				Data: []Challenge{
					{ID: "web-pasteur", Name: "Pasteur", Category: "web"},
				},
			})
			return
		}
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/challs/") {
			deletedID = strings.TrimPrefix(r.URL.Path, "/api/v1/challs/")
			_ = json.NewEncoder(w).Encode(Response[any]{Kind: "goodChallenge"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	plat, err := NewPlatform(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 1. Resolve target
	target, err := plat.ResolveUnsync(ctx, "web", "pasteur", "Pasteur")
	if err != nil {
		t.Fatalf("ResolveUnsync failed: %v", err)
	}
	if target.ID != "web-pasteur" {
		t.Errorf("target.ID = %q, want web-pasteur", target.ID)
	}

	// 2. Unsync
	if err := plat.Unsync(ctx, target); err != nil {
		t.Fatalf("Unsync failed: %v", err)
	}
	if deletedID != "web-pasteur" {
		t.Errorf("deletedID = %q, want web-pasteur", deletedID)
	}

	// 3. Resolve not found
	_, err = plat.ResolveUnsync(ctx, "pwn", "missing", "Missing")
	if err == nil || !strings.Contains(err.Error(), "no rCTF challenge matched") {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestPlatform_Configure(t *testing.T) {
	var receivedConfig AdminConfigUpdate
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/api/v1/admin/config" {
			_ = json.NewDecoder(r.Body).Decode(&receivedConfig)
			_ = json.NewEncoder(w).Encode(Response[any]{Kind: "goodConfig"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plat, err := NewPlatform(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	err = plat.Configure(context.Background(), platform.EventConfig{
		Name:        "Test CTF",
		Description: "A great CTF",
		StartAt:     "2026-11-12T10:00:00Z",
		EndAt:       "2026-11-13T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if receivedConfig.Meta.Name != "Test CTF" {
		t.Errorf("Name = %q, want Test CTF", receivedConfig.Meta.Name)
	}
	if receivedConfig.Meta.StartTime == nil || *receivedConfig.Meta.StartTime == 0 {
		t.Errorf("StartTime is nil or 0")
	}
	if receivedConfig.Meta.EndTime == nil || *receivedConfig.Meta.EndTime == 0 {
		t.Errorf("EndTime is nil or 0")
	}
}
