package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	expectedImage := "image: registry.example.com/mctf/mctf-test-web-chall:${IMAGE_TAG}"
	if !strings.Contains(outStr, expectedImage) {
		t.Errorf("expected generated image tag %q, but not found in: %s", expectedImage, outStr)
	}
}
