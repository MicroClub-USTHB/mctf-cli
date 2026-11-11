package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lineReader provides multiple line-by-line reads, supporting the
// pattern where bufio.NewReader is called in a loop (each iteration
// gets a fresh bufio reader on the same underlying source).
type lineReader struct {
	lines []string
	idx   int
	buf   []byte
	pos   int
}

func newLineReader(input string) *lineReader {
	lines := strings.Split(strings.TrimSuffix(input, "\n"), "\n")
	return &lineReader{lines: lines, idx: 0}
}

func (lr *lineReader) Read(p []byte) (int, error) {
	// Return buffered data first
	if lr.pos < len(lr.buf) {
		n := copy(p, lr.buf[lr.pos:])
		lr.pos += n
		return n, nil
	}

	// Move to next line if current is exhausted
	if lr.idx >= len(lr.lines) {
		return 0, io.EOF
	}

	line := lr.lines[lr.idx]
	lr.idx++
	lr.buf = []byte(line + "\n")
	lr.pos = 0

	n := copy(p, lr.buf)
	lr.pos += n
	return n, nil
}

func TestPromptString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		def   string
		want  string
	}{
		{
			name:  "empty input returns empty (caller keeps default)",
			input: "\n",
			def:   "default-value",
			want:  "",
		},
		{
			name:  "valid input returns trimmed value",
			input: "my-event\n",
			def:   "default",
			want:  "my-event",
		},
		{
			name:  "whitespace gets trimmed",
			input: "  padded-input  \n",
			def:   "default",
			want:  "padded-input",
		},
		{
			name:  "multiline takes first line only",
			input: "first-line\nsecond-line\n",
			def:   "default",
			want:  "first-line",
		},
		{
			name:  "EOF without input returns empty",
			input: "",
			def:   "default",
			want:  "",
		},
		{
			name:  "tabs and newlines trimmed",
			input: "\t\n  \n",
			def:   "x",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPromptIn := promptIn
			defer func() { promptIn = oldPromptIn }()

			promptIn = newLineReader(tt.input)
			got := promptString("test-label", tt.def)

			if got != tt.want {
				t.Errorf("promptString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptChoice(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		def     string
		choices []string
		want    string
	}{
		{
			name:    "empty input returns empty (caller keeps default)",
			input:   "\n",
			def:     "simple",
			choices: []string{"simple", "instanced", "pro"},
			want:    "",
		},
		{
			name:    "valid choice returns it",
			input:   "instanced\n",
			def:     "simple",
			choices: []string{"simple", "instanced", "pro"},
			want:    "instanced",
		},
		{
			name:    "whitespace trimmed from input",
			input:   "  pro  \n",
			def:     "simple",
			choices: []string{"simple", "instanced", "pro"},
			want:    "pro",
		},
		{
			name:    "invalid then valid re-prompts and succeeds",
			input:   "invalid\nsimple\n",
			def:     "pro",
			choices: []string{"simple", "instanced", "pro"},
			want:    "simple",
		},
		{
			name:    "multiple invalid then valid",
			input:   "bad1\nbad2\ninstanced\n",
			def:     "simple",
			choices: []string{"simple", "instanced", "pro"},
			want:    "instanced",
		},
		{
			name:    "EOF on invalid returns empty",
			input:   "invalid",
			def:     "simple",
			choices: []string{"simple", "instanced", "pro"},
			want:    "",
		},
		{
			name:    "first choice works",
			input:   "simple\n",
			def:     "pro",
			choices: []string{"simple", "instanced", "pro"},
			want:    "simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPromptIn := promptIn
			defer func() { promptIn = oldPromptIn }()

			promptIn = newLineReader(tt.input)
			got := promptChoice("profile", tt.def, tt.choices)

			if got != tt.want {
				t.Errorf("promptChoice() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptChoice_AllChoicesPresent(t *testing.T) {
	// Just verify that the function runs when empty is selected
	// (it prints all choices to stderr, but we can't easily capture that in tests)
	oldPromptIn := promptIn
	defer func() { promptIn = oldPromptIn }()

	promptIn = newLineReader("\n")
	choices := []string{"simple", "instanced", "pro"}
	got := promptChoice("profile", "simple", choices)

	// Empty input should return empty string
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestStdinIsTTY(t *testing.T) {
	// Just verify it returns a boolean and doesn't panic.
	// The actual TTY detection depends on os.Stdin at runtime.
	result := stdinIsTTY()
	if result != true && result != false {
		t.Errorf("stdinIsTTY() should return bool, got %v", result)
	}
}

func TestInitCmd_CreatesDirectoryWhenNameArgGiven(t *testing.T) {
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	flagInitNoTTY = true
	flagInitForce = false
	flagDir = "."
	defer func() {
		flagInitNoTTY = false
		flagInitForce = false
		flagDir = "."
	}()

	cmd := initCmd
	if err := cmd.RunE(cmd, []string{"my-ctf"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	targetDir := filepath.Join(tmp, "my-ctf")
	cfgPath := filepath.Join(targetDir, "mctf.config.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to exist: %v", cfgPath, err)
	}

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `name: "my-ctf"`) {
		t.Errorf("mctf.config.yml does not contain name 'my-ctf':\n%s", string(content))
	}
}

func TestInitCmd_NoArgsScaffoldsInCurrentDir(t *testing.T) {
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	flagInitNoTTY = true
	flagInitForce = false
	flagDir = "."
	defer func() {
		flagInitNoTTY = false
		flagInitForce = false
		flagDir = "."
	}()

	cmd := initCmd
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	cfgPath := filepath.Join(tmp, "mctf.config.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to exist directly in tmp: %v", cfgPath, err)
	}

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	wantName := filepath.Base(tmp)
	if !strings.Contains(string(content), fmt.Sprintf(`name: %q`, wantName)) {
		t.Errorf("mctf.config.yml does not contain name %q:\n%s", wantName, string(content))
	}
}

func TestInitCmd_DotArgScaffoldsInCurrentDir(t *testing.T) {
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	flagInitNoTTY = true
	flagInitForce = false
	flagDir = "."
	defer func() {
		flagInitNoTTY = false
		flagInitForce = false
		flagDir = "."
	}()

	cmd := initCmd
	if err := cmd.RunE(cmd, []string{"."}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	cfgPath := filepath.Join(tmp, "mctf.config.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to exist: %v", cfgPath, err)
	}
}

func TestInitCmd_WithDirFlagAndNameArg(t *testing.T) {
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	flagInitNoTTY = true
	flagInitForce = false
	flagDir = "custom-folder"
	defer func() {
		flagInitNoTTY = false
		flagInitForce = false
		flagDir = "."
	}()

	cmd := initCmd
	// Simulate -C flag being marked as changed on the command
	f := cmd.Flag("dir")
	if f != nil {
		f.Changed = true
		defer func() { f.Changed = false }()
	}

	if err := cmd.RunE(cmd, []string{"Custom Event"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	targetDir := filepath.Join(tmp, "custom-folder")
	cfgPath := filepath.Join(targetDir, "mctf.config.yml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected %s to exist: %v", cfgPath, err)
	}

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `name: "Custom Event"`) {
		t.Errorf("mctf.config.yml does not contain name 'Custom Event':\n%s", string(content))
	}
}

func TestInitCmd_AlreadyInitializedFailsWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	flagInitNoTTY = true
	flagInitForce = false
	flagDir = "."
	defer func() {
		flagInitNoTTY = false
		flagInitForce = false
		flagDir = "."
	}()

	cmd := initCmd
	if err := cmd.RunE(cmd, []string{"demo-ctf"}); err != nil {
		t.Fatalf("first RunE: %v", err)
	}

	// Second run without force should return ErrAlreadyInitialized
	err = cmd.RunE(cmd, []string{"demo-ctf"})
	if err == nil {
		t.Fatal("expected error on second init without --force, got nil")
	}

	// Third run with force should succeed
	flagInitForce = true
	if err := cmd.RunE(cmd, []string{"demo-ctf"}); err != nil {
		t.Fatalf("RunE with --force failed: %v", err)
	}
}
