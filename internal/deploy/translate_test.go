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

	expectedImage := "image: registry.example.com/mctf/test-web-chall:${IMAGE_TAG}"
	if !strings.Contains(outStr, expectedImage) {
		t.Errorf("expected generated image tag %q, but not found in: %s", expectedImage, outStr)
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
	expectedWebImage := "image: registry.example.com/evt-web-chall:${IMAGE_TAG}"
	if !strings.Contains(outStr, expectedWebImage) {
		t.Errorf("expected web to have %q, but got:\n%s", expectedWebImage, outStr)
	}
	// Verify it unmarshals as valid YAML without duplicate keys
	var parsed map[string]any
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("generated compose is invalid YAML: %v", err)
	}
}
