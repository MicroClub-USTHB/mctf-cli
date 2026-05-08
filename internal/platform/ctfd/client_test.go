package ctfd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer is a minimal in-memory CTFd stand-in. It records requests
// and responds with the canned envelopes the tests expect.
type fakeServer struct {
	t        *testing.T
	requests []*http.Request
	bodies   [][]byte
	handler  func(w http.ResponseWriter, r *http.Request)
}

func newFake(t *testing.T) (*fakeServer, *Client, func()) {
	t.Helper()
	f := &fakeServer{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.requests = append(f.requests, r)
		f.bodies = append(f.bodies, body)
		// Restore body so handlers can re-read (e.g. ParseMultipartForm).
		r.Body = io.NopCloser(bytes.NewReader(body))
		if f.handler != nil {
			f.handler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true, "data": {}}`))
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "test-token")
	return f, c, srv.Close
}

func TestListChallenges_DecodesEnvelope(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/challenges" || r.URL.Query().Get("view") != "admin" {
			t.Errorf("unexpected request: %s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{
            "success": true,
            "data": [
                {"id": 1, "name": "alpha", "category": "web", "type": "dynamic"},
                {"id": 2, "name": "beta", "category": "crypto", "type": "standard", "value": 100}
            ]
        }`))
	}
	got, err := c.ListChallenges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Type != "standard" {
		t.Errorf("decoded wrong: %+v", got)
	}
}

func TestSetsAuthAndContentType(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token test-token" {
			t.Errorf("auth header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		_, _ = w.Write([]byte(`{"success": true, "data": {"id": 7, "name": "x", "category": "web", "type": "dynamic"}}`))
	}
	if _, err := c.CreateChallenge(context.Background(), Challenge{Name: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateChallenge_PostsJSONBody(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": true, "data": {"id": 99, "name": "x", "category": "web", "type": "dynamic"}}`))
	}
	in := Challenge{Name: "x", Category: "web", Type: "dynamic", Description: "d"}
	out, err := c.CreateChallenge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 99 {
		t.Errorf("ID = %d", out.ID)
	}
	var body Challenge
	if err := json.Unmarshal(f.bodies[0], &body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body.Name != "x" || body.Category != "web" || body.Type != "dynamic" {
		t.Errorf("posted body = %+v", body)
	}
}

func TestAddFlag_StampsCaseSensitive(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": true, "data": {"id": 1, "challenge_id": 5}}`))
	}
	if _, err := c.AddFlag(context.Background(), 5, "mctf{x}", true); err != nil {
		t.Fatal(err)
	}
	var got Flag
	_ = json.Unmarshal(f.bodies[0], &got)
	if got.Type != "static" || got.Content != "mctf{x}" || got.Data != "case_sensitive" || got.ChallengeID != 5 {
		t.Errorf("flag body = %+v", got)
	}

	if _, err := c.AddFlag(context.Background(), 6, "y", false); err != nil {
		t.Fatal(err)
	}
	var got2 Flag
	_ = json.Unmarshal(f.bodies[1], &got2)
	if got2.Data != "" {
		t.Errorf("case-insensitive flag should leave Data empty, got %q", got2.Data)
	}
}

func TestErrorResponse_BecomesAPIError(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success": false, "errors": {"name": ["already taken"]}}`))
	}
	_, err := c.CreateChallenge(context.Background(), Challenge{Name: "dup"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Status != 400 || !strings.Contains(apiErr.Body, "already taken") {
		t.Errorf("api err = %+v", apiErr)
	}
}

func TestSuccessFalseOn200_StillError(t *testing.T) {
	// CTFd sometimes returns 200 with success=false; treat that as an error.
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": false, "data": {}, "message": "nope"}`))
	}
	_, err := c.CreateChallenge(context.Background(), Challenge{Name: "x"})
	if err == nil {
		t.Fatal("expected error for success=false")
	}
}

func TestDelete_NoBody(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}
	if err := c.DeleteFlag(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if f.requests[0].Method != "DELETE" || f.requests[0].URL.Path != "/api/v1/flags/42" {
		t.Errorf("wrong request: %s %s", f.requests[0].Method, f.requests[0].URL.Path)
	}
}

func TestUploadFile_SendsMultipart(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files" || r.Method != "POST" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Errorf("content-type = %q", ct)
		}
		// Parse the multipart body and verify fields.
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("challenge_id"); got != "5" {
			t.Errorf("challenge_id = %q", got)
		}
		if got := r.FormValue("type"); got != "challenge" {
			t.Errorf("type = %q", got)
		}
		fhs, ok := r.MultipartForm.File["file"]
		if !ok || len(fhs) != 1 {
			t.Fatalf("file field missing")
		}
		if fhs[0].Filename != "handout.zip" {
			t.Errorf("filename = %q", fhs[0].Filename)
		}
		_, _ = w.Write([]byte(`{"success": true, "data": [{"id": 9, "type": "challenge", "location": "uploads/abcd/handout.zip"}]}`))
	}
	out, err := c.UploadFile(context.Background(), 5, "handout.zip", strings.NewReader("zip-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 9 || out.Basename() != "handout.zip" {
		t.Errorf("uploaded file = %+v", out)
	}
}

func TestUploadFile_ErrorResponse(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success": false, "errors": {"file": ["too big"]}}`))
	}
	_, err := c.UploadFile(context.Background(), 5, "x.zip", strings.NewReader("body"))
	var api *APIError
	if !errors.As(err, &api) || !strings.Contains(api.Body, "too big") {
		t.Errorf("want APIError with too-big, got %v", err)
	}
}

func TestListFiles_FiltersByChallenge(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
            "success": true,
            "data": [
                {"id": 1, "type": "challenge", "location": "uploads/a/x.zip", "challenge_id": 5},
                {"id": 2, "type": "challenge", "location": "uploads/b/y.zip", "challenge_id": 6},
                {"id": 3, "type": "challenge", "location": "uploads/c/z.zip", "challenge_id": 5}
            ]
        }`))
	}
	got, err := c.ListFiles(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 files for challenge 5, got %d", len(got))
	}
	for _, file := range got {
		if file.ChallengeID != 5 {
			t.Errorf("filtered wrong: %+v", file)
		}
	}
}

func TestFile_Basename(t *testing.T) {
	cases := map[string]string{
		"uploads/abcd/handout.zip": "handout.zip",
		"plain.txt":                "plain.txt",
		"":                         "",
	}
	for loc, want := range cases {
		got := File{Location: loc}.Basename()
		if got != want {
			t.Errorf("Basename(%q) = %q, want %q", loc, got, want)
		}
	}
}

func TestSetConfig_PATCHesValue(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/configs/ctf_name" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success": true, "data": {"key": "ctf_name", "value": "MCTF 6.0"}}`))
	}
	if err := c.SetConfig(context.Background(), "ctf_name", "MCTF 6.0"); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	_ = json.Unmarshal(f.bodies[0], &got)
	if got["value"] != "MCTF 6.0" {
		t.Errorf("body = %+v", got)
	}
}

func TestGetConfig_DecodesValue(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": true, "data": {"key": "ctf_name", "value": "Hello"}}`))
	}
	got, err := c.GetConfig(context.Background(), "ctf_name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello" {
		t.Errorf("config value = %q", got)
	}
}

func TestUpdateChallenge_PATCH(t *testing.T) {
	f, c, _ := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": true, "data": {"id": 7, "name": "renamed", "category": "web", "type": "standard"}}`))
	}
	out, err := c.UpdateChallenge(context.Background(), 7, Challenge{Name: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if f.requests[0].Method != "PATCH" {
		t.Errorf("method = %s", f.requests[0].Method)
	}
	if out.Name != "renamed" {
		t.Errorf("name = %q", out.Name)
	}
}
