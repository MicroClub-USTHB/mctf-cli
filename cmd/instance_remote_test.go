package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
)

func TestRemoteInstanceSpawn(t *testing.T) {
	var gotKey, gotPath, gotMethod string
	var gotReq instancer.AdminSpawnRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"instance_id": "inst-remote-123",
			"challenge_id": "web/sqli",
			"team_id": "team-42",
			"status": "running",
			"ports": {"80": 32768},
			"public_url": "http://sqli.ctf.example.com:32768",
			"expires_at": "2026-10-13T16:00:00Z"
		}`))
	}))
	defer srv.Close()

	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "super-secret-admin-key")

	err := spawnRemoteInstance(context.Background(), srv.URL, "web", "sqli", "team-42", "mctf{custom}")
	if err != nil {
		t.Fatalf("spawnRemoteInstance returned unexpected error: %v", err)
	}

	if gotKey != "super-secret-admin-key" {
		t.Errorf("got X-Admin-Key %q, want super-secret-admin-key", gotKey)
	}
	if gotMethod != http.MethodPost || gotPath != "/admin/api/instances/spawn" {
		t.Errorf("got %s %s, want POST /admin/api/instances/spawn", gotMethod, gotPath)
	}
	if gotReq.ChallengeID != "web/sqli" || gotReq.TeamID != "team-42" || gotReq.Flag != "mctf{custom}" {
		t.Errorf("unexpected spawn request payload: %+v", gotReq)
	}
}

func TestRemoteInstanceExtend(t *testing.T) {
	var gotKey, gotPath, gotMethod string
	var gotReq instancer.AdminExtendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"message": "extended",
			"expires_at": "2026-10-13T17:00:00Z"
		}`))
	}))
	defer srv.Close()

	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "admin-key-extend")

	err := extendRemoteInstance(context.Background(), srv.URL, "inst-remote-123", 30*time.Minute)
	if err != nil {
		t.Fatalf("extendRemoteInstance returned unexpected error: %v", err)
	}

	if gotKey != "admin-key-extend" {
		t.Errorf("got X-Admin-Key %q, want admin-key-extend", gotKey)
	}
	if gotMethod != http.MethodPost || gotPath != "/admin/api/instances/inst-remote-123/extend" {
		t.Errorf("got %s %s, want POST /admin/api/instances/inst-remote-123/extend", gotMethod, gotPath)
	}
	if gotReq.DurationMinutes != 30 {
		t.Errorf("got duration_minutes %d, want 30", gotReq.DurationMinutes)
	}
}

func TestRemoteInstanceFlag(t *testing.T) {
	var gotKey, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"flag": "mctf{team_42_remote_flag}"
		}`))
	}))
	defer srv.Close()

	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "admin-key-flag")

	err := flagRemoteInstance(context.Background(), srv.URL, "inst-remote-123")
	if err != nil {
		t.Fatalf("flagRemoteInstance returned unexpected error: %v", err)
	}

	if gotKey != "admin-key-flag" {
		t.Errorf("got X-Admin-Key %q, want admin-key-flag", gotKey)
	}
	if gotMethod != http.MethodGet || gotPath != "/admin/api/instances/inst-remote-123/flag" {
		t.Errorf("got %s %s, want GET /admin/api/instances/inst-remote-123/flag", gotMethod, gotPath)
	}
}

func TestRemoteInstancePs(t *testing.T) {
	var gotKey, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Admin-Key")
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"instances": [
				{
					"instance_id": "inst-remote-123",
					"challenge_id": "web/sqli",
					"team_id": "team-42",
					"team_name": "CyberKnights",
					"status": "running",
					"ports": {"80": 32768},
					"public_url": "http://sqli.ctf.example.com:32768",
					"expires_at": "2026-10-13T16:00:00Z"
				}
			]
		}`))
	}))
	defer srv.Close()

	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "admin-key-ps")

	err := psRemoteInstance(context.Background(), srv.URL, "inst-remote-123")
	if err != nil {
		t.Fatalf("psRemoteInstance returned unexpected error: %v", err)
	}

	if gotKey != "admin-key-ps" {
		t.Errorf("got X-Admin-Key %q, want admin-key-ps", gotKey)
	}
	if gotMethod != http.MethodGet || gotPath != "/admin/api/instances" {
		t.Errorf("got %s %s, want GET /admin/api/instances", gotMethod, gotPath)
	}
}

func TestRemoteInstanceMissingAdminKeyFailsCleanly(t *testing.T) {
	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "")

	err := spawnRemoteInstance(context.Background(), "http://localhost:8080", "web", "sqli", "team-42", "")
	if err == nil || !strings.Contains(err.Error(), "MCTF_INSTANCER_ADMIN_KEY is not set") {
		t.Errorf("expected error mentioning MCTF_INSTANCER_ADMIN_KEY is not set, got %v", err)
	}

	err = extendRemoteInstance(context.Background(), "http://localhost:8080", "inst-1", 15*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "MCTF_INSTANCER_ADMIN_KEY is not set") {
		t.Errorf("expected error mentioning MCTF_INSTANCER_ADMIN_KEY is not set, got %v", err)
	}

	err = flagRemoteInstance(context.Background(), "http://localhost:8080", "inst-1")
	if err == nil || !strings.Contains(err.Error(), "MCTF_INSTANCER_ADMIN_KEY is not set") {
		t.Errorf("expected error mentioning MCTF_INSTANCER_ADMIN_KEY is not set, got %v", err)
	}
}

func TestRemoteInstancePsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instances":[]}`))
	}))
	defer srv.Close()

	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "admin-key")

	err := psRemoteInstance(context.Background(), srv.URL, "nonexistent-id")
	if err == nil || !strings.Contains(err.Error(), "no instance with id \"nonexistent-id\" found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestRemoteInstanceSpawnJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"instance_id": "inst-json-456",
			"challenge_id": "pwn/bof",
			"team_id": "team-99",
			"status": "running"
		}`))
	}))
	defer srv.Close()

	t.Setenv("MCTF_INSTANCER_ADMIN_KEY", "admin-key-json")
	prevJSON := flagJSON
	flagJSON = true
	defer func() { flagJSON = prevJSON }()

	err := spawnRemoteInstance(context.Background(), srv.URL, "pwn", "bof", "team-99", "")
	if err != nil {
		t.Fatalf("unexpected error in JSON mode: %v", err)
	}
}
