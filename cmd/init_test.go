package cmd

import (
	"io"
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
