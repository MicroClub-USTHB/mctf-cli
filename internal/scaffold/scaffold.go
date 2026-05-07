package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// EventVars are the substitutions applied to *.tmpl files in the event
// template tree. All fields are required by the templates that reference
// them; callers should fill via NewEventVars to get sane defaults.
type EventVars struct {
	EventName  string
	EventSlug  string
	Domain     string
	FlagPrefix string
	Profile    string
}

// NewEventVars builds an EventVars from a free-form event name. Slug is
// derived by lower-casing and replacing non-alphanumerics with "-";
// FlagPrefix and Profile fall back to the spec defaults.
func NewEventVars(name string) EventVars {
	return EventVars{
		EventName:  name,
		EventSlug:  Slugify(name),
		Domain:     "ctf.example.com",
		FlagPrefix: "mctf",
		Profile:    "instanced",
	}
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify produces a DNS-label-compatible slug from a free-form name.
// Empty input yields "event".
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "event"
	}
	return s
}

// EventOptions controls scaffold behavior.
type EventOptions struct {
	// Force allows scaffolding into a directory that already contains
	// mtcf.config.yml; existing files are still skipped unless they
	// would be overwritten and Force is true.
	Force bool
}

// Result describes which files were written and which were skipped
// because they already existed (or because Force was off).
type Result struct {
	Written []string
	Skipped []string
}

// ErrAlreadyInitialized is returned when the target directory already
// contains an mtcf.config.yml and Force is false.
var ErrAlreadyInitialized = errors.New("event already initialized (mtcf.config.yml exists) — pass --force to overwrite")

// Event renders the embedded event template tree into dir, applying
// vars to *.tmpl files. dir is created if missing.
func Event(dir string, vars EventVars, opts EventOptions) (*Result, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	if !opts.Force {
		if _, err := os.Stat(filepath.Join(dir, "mtcf.config.yml")); err == nil {
			return nil, ErrAlreadyInitialized
		}
	}

	res := &Result{}
	root := "templates/event"
	err := fs.WalkDir(eventFS, root, func(srcPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == root {
			return nil
		}
		rel, err := filepath.Rel(root, srcPath)
		if err != nil {
			return err
		}
		dstRel := renameForOutput(rel)
		dst := filepath.Join(dir, dstRel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		if _, err := os.Stat(dst); err == nil && !opts.Force {
			res.Skipped = append(res.Skipped, dst)
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		data, err := fs.ReadFile(eventFS, srcPath)
		if err != nil {
			return err
		}

		if strings.HasSuffix(srcPath, ".tmpl") {
			out, err := render(srcPath, string(data), vars)
			if err != nil {
				return err
			}
			data = []byte(out)
		}

		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		res.Written = append(res.Written, dst)
		return nil
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// renameForOutput maps embedded paths to their on-disk destinations.
//
// Files prefixed with "_" become dotfiles, and ".tmpl" suffixes are
// stripped. Everything else passes through.
func renameForOutput(rel string) string {
	parts := strings.Split(rel, string(filepath.Separator))
	last := parts[len(parts)-1]
	switch last {
	case "_gitignore":
		last = ".gitignore"
	case "_env.example":
		last = ".env.example"
	default:
		last = strings.TrimSuffix(last, ".tmpl")
	}
	parts[len(parts)-1] = last
	return filepath.Join(parts...)
}

func render(name, body string, vars EventVars) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("%s: parse template: %w", name, err)
	}
	var sb strings.Builder
	if err := t.Execute(&sb, vars); err != nil {
		return "", fmt.Errorf("%s: render: %w", name, err)
	}
	return sb.String(), nil
}
