package status

import (
	"context"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

type fakeInspector struct {
	containers map[string][]composeContainer
	err        map[string]error
}

func (f fakeInspector) Inspect(_ context.Context, project, _ string) ([]composeContainer, error) {
	if f.err != nil {
		if e, ok := f.err[project]; ok {
			return nil, e
		}
	}
	if f.containers != nil {
		return f.containers[project], nil
	}
	return nil, nil
}

func cfg() *config.Config {
	return &config.Config{
		Version:   1,
		Event:     config.Event{Slug: "evt", Domain: "ctf.example.com"},
		Instancer: &config.Instancer{URL: "https://instancer.example.com"},
	}
}

func challs() []*challenge.Challenge {
	port := 14000
	internal := 8000
	return []*challenge.Challenge{
		{
			Slug: "rsa-baby", CategoryDir: "crypto",
			Manifest:  challenge.Manifest{Schema: 1, Challenge: challenge.ChallengeBlock{Name: "RSA"}},
			Archetype: challenge.ArchetypeStatic,
		},
		{
			Slug: "welcome", CategoryDir: "misc", ChallengeDir: "/x",
			Manifest: challenge.Manifest{
				Schema:    1,
				Challenge: challenge.ChallengeBlock{Name: "Welcome", NeedDeploy: true},
				Deploy:    &challenge.DeployBlock{Subdomain: "welcome", Port: &port, InternalPort: &internal},
			},
			Archetype: challenge.ArchetypeService,
		},
		{
			Slug: "cub3", CategoryDir: "web",
			Manifest: challenge.Manifest{
				Schema:    1,
				Challenge: challenge.ChallengeBlock{Name: "CUB3", NeedDeploy: true},
				Deploy:    &challenge.DeployBlock{Subdomain: "cub3", InternalPort: &internal, DynamicInstances: true},
			},
			Archetype: challenge.ArchetypeInstanced,
		},
	}
}

func TestRun_StaticAndInstancedHaveSyntheticState(t *testing.T) {
	rows, err := Run(context.Background(), cfg(), challs(), fakeInspector{})
	if err != nil {
		t.Fatal(err)
	}
	got := byChallenge(rows)
	if got["crypto/rsa-baby"].State != "static" {
		t.Errorf("static state = %q", got["crypto/rsa-baby"].State)
	}
	if got["web/cub3"].State != "instanced" {
		t.Errorf("instanced state = %q", got["web/cub3"].State)
	}
	if got["web/cub3"].URL != "https://instancer.example.com" {
		t.Errorf("instanced URL = %q", got["web/cub3"].URL)
	}
}

func TestRun_ServiceQueriesInspector(t *testing.T) {
	insp := fakeInspector{
		containers: map[string][]composeContainer{
			"mtcf-evt-misc-welcome": {
				{Name: "mtcf-evt-misc-welcome-app-1", State: "running", Image: "ghcr.io/x:latest", Status: "Up 5 minutes"},
			},
		},
	}
	rows, err := Run(context.Background(), cfg(), challs(), insp)
	if err != nil {
		t.Fatal(err)
	}
	got := byChallenge(rows)
	r := got["misc/welcome"]
	if r.State != "running" {
		t.Errorf("state = %q", r.State)
	}
	if r.Container != "mtcf-evt-misc-welcome-app-1" {
		t.Errorf("container = %q", r.Container)
	}
	if r.Uptime != "5 minutes" {
		t.Errorf("uptime = %q", r.Uptime)
	}
	if r.URL != "https://welcome.ctf.example.com:14000" {
		t.Errorf("url = %q", r.URL)
	}
}

func TestRun_ServiceMissingComposeReportsNotDeployed(t *testing.T) {
	rows, err := Run(context.Background(), cfg(), challs(), fakeInspector{})
	if err != nil {
		t.Fatal(err)
	}
	got := byChallenge(rows)
	if got["misc/welcome"].State != "not deployed" {
		t.Errorf("state = %q", got["misc/welcome"].State)
	}
}

func TestParseUptime(t *testing.T) {
	cases := map[string]string{
		"Up 5 minutes":           "5 minutes",
		"Up About a minute":      "About a minute",
		"Up 2 hours (healthy)":   "2 hours",
		"Exited (0) 2 hours ago": "",
		"":                       "",
	}
	for in, want := range cases {
		if got := parseUptime(in); got != want {
			t.Errorf("parseUptime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseComposeJSON_LineDelimited(t *testing.T) {
	body := `{"Name":"a","State":"running","Image":"x","Status":"Up 1m"}
{"Name":"b","State":"exited","Image":"y","Status":"Exited (0)"}`
	got, err := parseComposeJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].State != "exited" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestParseComposeJSON_Array(t *testing.T) {
	body := `[{"Name":"a","State":"running","Image":"x","Status":"Up 1m"}]`
	got, err := parseComposeJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestParseComposeJSON_Empty(t *testing.T) {
	got, err := parseComposeJSON([]byte("\n"))
	if err != nil || len(got) != 0 {
		t.Errorf("empty input: got %v err %v", got, err)
	}
}

func TestRender_HasSummary(t *testing.T) {
	rows := []Row{
		{Challenge: "misc/a", Archetype: "service", State: "running"},
		{Challenge: "misc/b", Archetype: "service", State: "exited"},
		{Challenge: "crypto/c", Archetype: "static", State: "static"},
	}
	out := Render(rows)
	for _, want := range []string{"misc/a", "misc/b", "crypto/c", "2 service(s)", "1 running", "1 not running"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
}

func byChallenge(rows []Row) map[string]Row {
	out := make(map[string]Row, len(rows))
	for _, r := range rows {
		out[r.Challenge] = r
	}
	return out
}
