package challenge

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadManifest asserts the manifest loader never panics on arbitrary
// file contents — malformed YAML, schema-invalid docs, huge/garbage input —
// and that a manifest it accepts can be fed to DeriveArchetype safely.
func FuzzLoadManifest(f *testing.F) {
	for _, s := range []string{
		"schema: 1\nchallenge:\n  name: x\n  category: web\n  need_deploy: false\n",
		"schema: 1\nchallenge:\n  name: y\n  category: web\n  need_deploy: true\ndeploy:\n  dynamic_instances: true\n",
		"",
		"not: [valid",
		"challenge: 123",
		"\x00\x01\x02",
		"schema: 999999999999999999999",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		p := filepath.Join(dir, "challenge.yml")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Skip()
		}
		m, _, err := LoadManifest(p)
		if err == nil {
			// A loaded manifest must derive an archetype without panicking.
			_ = DeriveArchetype(m)
		}
	})
}

// FuzzHasTopLevelYAMLKey asserts the raw-YAML key probe never panics.
func FuzzHasTopLevelYAMLKey(f *testing.F) {
	f.Add([]byte("schema: 1\n"), "schema")
	f.Add([]byte("a: [1,2"), "a")
	f.Add([]byte(""), "")
	f.Fuzz(func(t *testing.T, raw []byte, key string) {
		_ = HasTopLevelYAMLKey(raw, key)
	})
}
