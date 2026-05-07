package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalConfig = `version: 1
event:
  name: "Test"
  slug: "test-event"
  domain: "test.example.com"
profile: simple
deploy:
  mode: docker
platform:
  type: ctfd
challenges_dir: "./challenges"
`

func writeMinimalEvent(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "mtcf.config.yml"), []byte(minimalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "challenges"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRun_NoConfigFails(t *testing.T) {
	dir := t.TempDir()
	checks := Run(dir, Options{SkipEnv: true})
	if len(checks) == 0 {
		t.Fatal("no checks emitted")
	}
	if checks[0].ID != "D001" || checks[0].Status != StatusFail {
		t.Errorf("first check should fail D001, got %+v", checks[0])
	}
}

func TestRun_MinimalRepo_NoChallenges(t *testing.T) {
	dir := t.TempDir()
	writeMinimalEvent(t, dir)
	checks := Run(dir, Options{SkipEnv: true})
	got := byID(checks)
	if c, ok := got["D001"]; !ok || c.Status != StatusOK {
		t.Errorf("D001 should be ok: %+v", c)
	}
	if c, ok := got["D002"]; !ok || c.Status != StatusWarn {
		t.Errorf("D002 should be warn (no challenges): %+v", c)
	}
	// D003 should not appear because there are no challenges to validate.
	if _, ok := got["D003"]; ok {
		t.Errorf("D003 should not appear when no challenges discovered")
	}
}

func TestRun_SkipEnvOmitsEnvChecks(t *testing.T) {
	dir := t.TempDir()
	writeMinimalEvent(t, dir)
	checks := Run(dir, Options{SkipEnv: true})
	got := byID(checks)
	for _, id := range []string{"D010", "D011", "D012", "D013", "D020", "D021"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s should be skipped under SkipEnv", id)
		}
	}
}

func TestCounts(t *testing.T) {
	checks := []Check{
		{Status: StatusOK}, {Status: StatusOK},
		{Status: StatusFail},
		{Status: StatusWarn}, {Status: StatusWarn}, {Status: StatusWarn},
		{Status: StatusSkip},
	}
	ok, fail, warn, skip := Counts(checks)
	if ok != 2 || fail != 1 || warn != 3 || skip != 1 {
		t.Errorf("ok=%d fail=%d warn=%d skip=%d", ok, fail, warn, skip)
	}
}

func TestRender_IncludesAllRows(t *testing.T) {
	out := Render([]Check{
		{ID: "D001", Name: "x", Status: StatusOK, Detail: "fine"},
		{ID: "D999", Name: "broken", Status: StatusFail, Detail: "bad", Suggest: "fix it"},
	})
	for _, want := range []string{"D001", "D999", "fine", "bad", "fix it", "1 ok, 1 fail"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
}

func TestJSON_RoundTrips(t *testing.T) {
	body, err := JSON([]Check{{ID: "D001", Name: "x", Status: StatusOK}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Check
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	if len(got) != 1 || got[0].ID != "D001" {
		t.Errorf("round-tripped: %+v", got)
	}
}

func byID(cs []Check) map[string]Check {
	out := make(map[string]Check, len(cs))
	for _, c := range cs {
		out[c.ID] = c
	}
	return out
}
