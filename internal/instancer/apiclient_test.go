package instancer

import (
	"context"
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
