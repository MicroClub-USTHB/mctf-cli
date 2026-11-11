package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func TestNewPlatform_MissingBlock(t *testing.T) {
	_, err := newPlatform(&config.Config{})
	if err == nil || !strings.Contains(err.Error(), `must be one of ctfd, webhook (got "")`) {
		t.Errorf("err = %v", err)
	}
}

func TestNewPlatform_UnknownType(t *testing.T) {
	_, err := newPlatform(&config.Config{Platform: &config.Platform{Type: "rctf", URL: "https://x.example"}})
	if err == nil || !strings.Contains(err.Error(), `(got "rctf")`) {
		t.Errorf("err = %v", err)
	}
}

func TestNewPlatform_EmptyURL(t *testing.T) {
	_, err := newPlatform(&config.Config{Platform: &config.Platform{Type: "webhook"}})
	if err == nil || !strings.Contains(err.Error(), "platform.url is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestNewPlatform_CTFdMissingToken(t *testing.T) {
	t.Setenv("MCTF_CTFD_TOKEN", "")
	_, err := newPlatform(&config.Config{Platform: &config.Platform{Type: "ctfd", URL: "https://ctfd.example"}})
	if err == nil || !strings.Contains(err.Error(), "MCTF_CTFD_TOKEN env var is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestNewPlatform_WebhookHTTPRefused(t *testing.T) {
	cfg := &config.Config{Platform: &config.Platform{Type: "webhook", URL: "http://hooks.example/x"}}
	if _, err := newPlatform(cfg); err == nil || !strings.Contains(err.Error(), "--insecure-http") {
		t.Errorf("err = %v", err)
	}
	flagPlatformInsecureHTTP = true
	defer func() { flagPlatformInsecureHTTP = false }()
	if _, err := newPlatform(cfg); err != nil {
		t.Errorf("with --insecure-http: %v", err)
	}
}

func TestPlatformSync_WebhookDryRunNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := "version: 1\nevent:\n  name: \"Hook CTF\"\n  slug: \"hook-ctf\"\nplatform:\n  type: webhook\n  url: \"" + srv.URL + "/hook\"\n"
	if err := os.WriteFile(filepath.Join(dir, "mctf.config.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	chDir := filepath.Join(dir, "challenges", "misc", "hello", "challenge")
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `schema: 1
challenge:
  name: "hello"
  author: "alice"
  category: misc
  difficulty: easy
  description: "hi"
  wave: 1
  flags:
    - { flag: "mctf{hello}", case_sensitive: true }
  need_deploy: false
`
	if err := os.WriteFile(filepath.Join(chDir, "challenge.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	flagDir = dir
	flagSyncDryRun = true
	defer func() {
		flagDir = ""
		flagSyncDryRun = false
	}()
	if err := platformSyncCmd.RunE(platformSyncCmd, nil); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("dry-run sent %d request(s)", n)
	}
}
