package config

import (
	"os"
	"path/filepath"
	"testing"
)

func _writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }

func TestLoadSampleEvent(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "sample-event")
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Event.Slug != "sample-event" {
		t.Errorf("event.slug = %q, want %q", c.Event.Slug, "sample-event")
	}
	if c.Profile != ProfileInstanced {
		t.Errorf("profile = %q, want %q", c.Profile, ProfileInstanced)
	}
	if c.Deploy == nil || c.Deploy.Mode != "docker" {
		t.Errorf("deploy.mode = %v, want docker", c.Deploy)
	}
	if c.Event.FlagPrefix != "mctf" {
		t.Errorf("flag_prefix = %q, want mctf", c.Event.FlagPrefix)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestLoadInvalidProfile(t *testing.T) {
	dir := t.TempDir()
	bad := []byte(`
version: 1
event:
  name: "Bad"
  slug: "bad"
profile: chaos
`)
	if err := writeFile(filepath.Join(dir, FileName), bad); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected schema error for invalid profile")
	}
	if !contains(err.Error(), "/profile") {
		t.Errorf("error missing /profile path: %v", err)
	}
}

// trivial helpers (kept here to avoid importing extra packages just for tests)
func writeFile(path string, data []byte) error {
	return _writeFile(path, data)
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
