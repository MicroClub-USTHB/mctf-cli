package info

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func sampleStatic() *challenge.Challenge {
	return &challenge.Challenge{
		Slug:        "rsa-baby",
		CategoryDir: "crypto",
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: "RSA Baby", Author: "alice", Category: "crypto",
				Difficulty: "easy", Wave: 1, Description: "tiny RSA",
				Flags:      []challenge.Flag{{Flag: "mctf{x}", CaseSensitive: true}},
				NeedDeploy: false,
				Tags:       []string{"rsa", "math"},
			},
		},
		Archetype: challenge.ArchetypeStatic,
	}
}

func sampleService() *challenge.Challenge {
	port := 14000
	return &challenge.Challenge{
		Slug: "welcome", CategoryDir: "misc",
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: "Welcome", Author: "bob", Category: "misc",
				Difficulty: "easy", Wave: 1,
				Flags:      []challenge.Flag{{Flag: "mctf{w}", CaseSensitive: true}},
				NeedDeploy: true,
			},
			Deploy: &challenge.DeployBlock{
				Subdomain: "welcome", Port: &port, DynamicInstances: false,
			},
		},
		Archetype: challenge.ArchetypeService,
	}
}

func sampleInstanced() *challenge.Challenge {
	internal := 8000
	return &challenge.Challenge{
		Slug: "cub3", CategoryDir: "web",
		Manifest: challenge.Manifest{
			Schema: 1,
			Challenge: challenge.ChallengeBlock{
				Name: "CUB3", Author: "carol", Category: "web",
				Difficulty: "medium", Wave: 1,
				Flags:      []challenge.Flag{{Flag: "mctf{c}", CaseSensitive: true}},
				NeedDeploy: true,
			},
			Deploy: &challenge.DeployBlock{
				Subdomain: "cub3", InternalPort: &internal, DynamicInstances: true,
			},
		},
		Archetype: challenge.ArchetypeInstanced,
	}
}

func sampleConfig() *config.Config {
	return &config.Config{
		Version: 1,
		Event: config.Event{
			Name: "Sample", Slug: "sample-event", Domain: "ctf.example.com",
		},
		Registry:  &config.Registry{URL: "ghcr.io/example"},
		Instancer: &config.Instancer{URL: "https://instancer.example.com"},
	}
}

func TestBuild_StaticHasNoIngressOrImage(t *testing.T) {
	v := Build(sampleConfig(), sampleStatic())
	if v.Archetype != "static" {
		t.Errorf("archetype = %q", v.Archetype)
	}
	if v.Ingress != nil {
		t.Errorf("static challenge should not have ingress: %+v", v.Ingress)
	}
	if v.Build.Image != "" {
		t.Errorf("static challenge should not have image tag: %q", v.Build.Image)
	}
	if v.Flag.Value != "mctf{x}" {
		t.Errorf("flag value = %q", v.Flag.Value)
	}
}

func TestBuild_ServiceProducesURLAndImage(t *testing.T) {
	v := Build(sampleConfig(), sampleService())
	if v.Ingress == nil {
		t.Fatal("service must have ingress")
	}
	if v.Ingress.Mode != "service" {
		t.Errorf("mode = %q", v.Ingress.Mode)
	}
	if v.Ingress.URL != "https://welcome.ctf.example.com:14000" {
		t.Errorf("url = %q", v.Ingress.URL)
	}
	if v.Build.Image != "ghcr.io/example/sample-event-misc-welcome:<sha>" {
		t.Errorf("image = %q", v.Build.Image)
	}
}

func TestBuild_InstancedRoutesThroughInstancer(t *testing.T) {
	v := Build(sampleConfig(), sampleInstanced())
	if v.Ingress == nil || v.Ingress.Mode != "instanced" {
		t.Fatalf("ingress = %+v", v.Ingress)
	}
	if v.Ingress.URL != "https://instancer.example.com" {
		t.Errorf("url = %q", v.Ingress.URL)
	}
	if v.Ingress.InternalPort != 8000 {
		t.Errorf("internal_port = %d", v.Ingress.InternalPort)
	}
	if v.Build.Image == "" {
		t.Errorf("instanced challenge should have image tag")
	}
}

func TestRender_ContainsKeyFields(t *testing.T) {
	v := Build(sampleConfig(), sampleService())
	out := v.Render()
	for _, want := range []string{
		"misc/welcome",
		"name",
		"Welcome",
		"archetype",
		"service",
		"https://welcome.ctf.example.com:14000",
		"ghcr.io/example/sample-event-misc-welcome:<sha>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
}

func TestJSON_RoundTrips(t *testing.T) {
	v := Build(sampleConfig(), sampleStatic())
	b, err := v.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var got View
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, b)
	}
	if got.Slug != "rsa-baby" {
		t.Errorf("round-tripped slug = %q", got.Slug)
	}
}
