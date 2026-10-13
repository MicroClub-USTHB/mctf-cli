package instancer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIClientList(t *testing.T) {
	var gotKey, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instances":[
			{"instance_id":"echo-1","challenge_id":"web/echo","team_id":"5","team_name":"TeamX",
			 "status":"running","ports":{"80":10234},"expires_at":"2026-10-12T00:00:00Z"}
		]}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "secret")
	got, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "secret" {
		t.Errorf("admin key header = %q, want secret", gotKey)
	}
	if gotMethod != http.MethodGet || gotPath != "/admin/api/instances" {
		t.Errorf("request = %s %s, want GET /admin/api/instances", gotMethod, gotPath)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(got))
	}
	in := got[0]
	if in.InstanceID != "echo-1" || in.ChallengeID != "web/echo" || in.TeamID != "5" {
		t.Errorf("unexpected instance fields: %+v", in)
	}
	if in.Ports["80"] != 10234 {
		t.Errorf("ports not parsed: %+v", in.Ports)
	}
}

func TestAPIClientDestroy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/admin/api/instances/echo-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"stopped"}`))
	}))
	defer srv.Close()

	msg, err := NewAPIClient(srv.URL, "k").Destroy(context.Background(), "echo-1")
	if err != nil {
		t.Fatal(err)
	}
	if msg != "stopped" {
		t.Errorf("message = %q, want stopped", msg)
	}
}

func TestAPIClientDestroyFailureSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"message":"instance not found"}`))
	}))
	defer srv.Close()

	_, err := NewAPIClient(srv.URL, "k").Destroy(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "instance not found") {
		t.Errorf("expected failure message surfaced, got %v", err)
	}
}

func TestAPIClientAuthErrorIsReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Invalid admin key"}`))
	}))
	defer srv.Close()

	_, err := NewAPIClient(srv.URL, "wrong").List(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	// Should carry both the server detail and the fix hint.
	if !strings.Contains(err.Error(), "Invalid admin key") || !strings.Contains(err.Error(), "MCTF_INSTANCER_ADMIN_KEY") {
		t.Errorf("unhelpful auth error: %v", err)
	}
}

func TestAPIClientEmptyURL(t *testing.T) {
	_, err := NewAPIClient("", "k").List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "url is not set") {
		t.Errorf("expected url-not-set error, got %v", err)
	}
}

func TestAPIClientMissingAdminKey(t *testing.T) {
	_, err := NewAPIClient("https://instancer.example.com", "").List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "MCTF_INSTANCER_ADMIN_KEY is not set") {
		t.Errorf("expected missing admin key error, got %v", err)
	}
}

func TestAPIClientSpawn(t *testing.T) {
	var gotKey, gotMethod, gotPath, gotContentType string
	var gotBody AdminSpawnRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotContentType = r.Header.Get("Content-Type")
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"instance_id": "echo-inst-1",
			"challenge_id": "web/echo",
			"team_id": "42",
			"status": "running",
			"ports": {"80": 30001},
			"public_url": "http://echo.example.com:30001",
			"expires_at": "2026-10-13T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "admin-secret")
	req := AdminSpawnRequest{
		ChallengeID: "web/echo",
		TeamID:      "42",
		UserID:      "42",
		Flag:        "mctf{custom}",
	}
	inst, err := c.Spawn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "admin-secret" {
		t.Errorf("admin key = %q, want admin-secret", gotKey)
	}
	if gotMethod != http.MethodPost || gotPath != "/admin/api/instances/spawn" {
		t.Errorf("request = %s %s, want POST /admin/api/instances/spawn", gotMethod, gotPath)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if gotBody.ChallengeID != "web/echo" || gotBody.TeamID != "42" || gotBody.Flag != "mctf{custom}" {
		t.Errorf("unexpected body parsed: %+v", gotBody)
	}
	if inst.InstanceID != "echo-inst-1" || inst.Ports["80"] != 30001 {
		t.Errorf("unexpected instance returned: %+v", inst)
	}
}

func TestAPIClientExtend(t *testing.T) {
	var gotKey, gotMethod, gotPath string
	var gotBody AdminExtendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"message": "extended",
			"expires_at": "2026-10-13T12:30:00Z"
		}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "admin-secret")
	expires, err := c.Extend(context.Background(), "echo-inst-1", 15)
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "admin-secret" {
		t.Errorf("admin key = %q, want admin-secret", gotKey)
	}
	if gotMethod != http.MethodPost || gotPath != "/admin/api/instances/echo-inst-1/extend" {
		t.Errorf("request = %s %s, want POST /admin/api/instances/echo-inst-1/extend", gotMethod, gotPath)
	}
	if gotBody.DurationMinutes != 15 {
		t.Errorf("duration_minutes = %d, want 15", gotBody.DurationMinutes)
	}
	if expires != "2026-10-13T12:30:00Z" {
		t.Errorf("expires = %q, want 2026-10-13T12:30:00Z", expires)
	}
}

func TestAPIClientFlag(t *testing.T) {
	var gotKey, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"flag": "mctf{team_flag_123}"
		}`))
	}))
	defer srv.Close()

	c := NewAPIClient(srv.URL, "admin-secret")
	flag, err := c.Flag(context.Background(), "echo-inst-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "admin-secret" {
		t.Errorf("admin key = %q, want admin-secret", gotKey)
	}
	if gotMethod != http.MethodGet || gotPath != "/admin/api/instances/echo-inst-1/flag" {
		t.Errorf("request = %s %s, want GET /admin/api/instances/echo-inst-1/flag", gotMethod, gotPath)
	}
	if flag != "mctf{team_flag_123}" {
		t.Errorf("flag = %q, want mctf{team_flag_123}", flag)
	}
}
