package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTranslateCompose(t *testing.T) {
	cwd := t.TempDir()
	originalCompose := filepath.Join(cwd, "docker-compose.yml")

	yamlContent := `
services:
  web:
    build: .
    ports:
      - "80:80"
  db:
    image: postgres:15
    build:
      context: ./db
`
	if err := os.WriteFile(originalCompose, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	u := Unit{
		ComposeFile: originalCompose,
		Cwd:         cwd,
		ProjectName: "mctf-test-web-chall",
		ImageTag:    "a1b2c3d",
	}

	err := TranslateCompose(u, "registry.example.com/mctf")
	if err != nil {
		t.Fatalf("TranslateCompose failed: %v", err)
	}

	mctfCompose := filepath.Join(cwd, "docker-compose.mctf.yml")
	out, err := os.ReadFile(mctfCompose)
	if err != nil {
		t.Fatalf("could not read generated compose file: %v", err)
	}

	outStr := string(out)
	if strings.Contains(outStr, "build:") {
		t.Errorf("expected 'build:' blocks to be removed, but found in: %s", outStr)
	}

	// The image is pinned to the literal tag — not a ${IMAGE_TAG} variable —
	// so the generated file is a self-contained, reproducible artifact.
	expectedImage := "image: registry.example.com/mctf/test-web-chall:a1b2c3d"
	if !strings.Contains(outStr, expectedImage) {
		t.Errorf("expected generated image tag %q, but not found in: %s", expectedImage, outStr)
	}
	if strings.Contains(outStr, "${IMAGE_TAG}") {
		t.Errorf("image tag must be literal, not a variable, in: %s", outStr)
	}
}

func TestTranslateCompose_EmptyTagFallsBackToLatest(t *testing.T) {
	cwd := t.TempDir()
	originalCompose := filepath.Join(cwd, "docker-compose.yml")
	if err := os.WriteFile(originalCompose, []byte("services:\n  web:\n    build: .\n"), 0644); err != nil {
		t.Fatal(err)
	}
	u := Unit{ComposeFile: originalCompose, Cwd: cwd, ProjectName: "mctf-evt-web-chall"} // no ImageTag
	if err := TranslateCompose(u, "registry.example.com"); err != nil {
		t.Fatalf("TranslateCompose failed: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(cwd, "docker-compose.mctf.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "image: registry.example.com/evt-web-chall:latest") {
		t.Errorf("empty ImageTag should fall back to :latest, got:\n%s", out)
	}
}

func TestTranslateCompose_PreservesThirdPartyServices(t *testing.T) {
	cwd := t.TempDir()
	originalCompose := filepath.Join(cwd, "docker-compose.yml")

	yamlContent := `services:
  web:
    build: .
    ports:
      - "80:80"
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: secret
`
	if err := os.WriteFile(originalCompose, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	u := Unit{
		ComposeFile: originalCompose,
		Cwd:         cwd,
		ProjectName: "mctf-evt-web-chall",
		ImageTag:    "deadbee",
	}

	err := TranslateCompose(u, "registry.example.com")
	if err != nil {
		t.Fatalf("TranslateCompose failed: %v", err)
	}

	mctfCompose := filepath.Join(cwd, "docker-compose.mctf.yml")
	out, err := os.ReadFile(mctfCompose)
	if err != nil {
		t.Fatalf("could not read generated compose file: %v", err)
	}

	outStr := string(out)
	// db must keep its original image and not be overwritten
	if !strings.Contains(outStr, "image: postgres:16") {
		t.Errorf("expected db to keep 'image: postgres:16', but got:\n%s", outStr)
	}
	// web must have the challenge image matching build.go (<registry>/<event>-<cat>-<slug>)
	expectedWebImage := "image: registry.example.com/evt-web-chall:deadbee"
	if !strings.Contains(outStr, expectedWebImage) {
		t.Errorf("expected web to have %q, but got:\n%s", expectedWebImage, outStr)
	}
	// Verify it unmarshals as valid YAML without duplicate keys
	var parsed map[string]any
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("generated compose is invalid YAML: %v", err)
	}
}
