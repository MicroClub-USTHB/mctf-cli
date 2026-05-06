// Package validate implements the cross-cutting checks the spec defines as
// hard-fails or warnings. Schema-level checks live in internal/config and
// internal/challenge — this package picks up where they leave off.
package validate

import (
	"fmt"
	"sort"
	"strings"
)

// Severity is one of the three reporting levels.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarn:
		return "warn"
	default:
		return "info"
	}
}

// Issue is a single validation finding.
type Issue struct {
	Severity Severity // SeverityError = hard fail
	Rule     string   // rule ID from docs/spec/validation.md, e.g. "S005"
	File     string   // file path the issue applies to (challenge dir if cross-cutting)
	Message  string   // human-readable explanation
}

// Report aggregates issues from one or more checkers.
type Report struct {
	Issues []Issue
}

// Add appends an issue to the report.
func (r *Report) Add(i Issue) {
	r.Issues = append(r.Issues, i)
}

// Errorf is sugar for Add(error severity).
func (r *Report) Errorf(rule, file, format string, args ...any) {
	r.Add(Issue{Severity: SeverityError, Rule: rule, File: file, Message: fmt.Sprintf(format, args...)})
}

// Warnf is sugar for Add(warn severity).
func (r *Report) Warnf(rule, file, format string, args ...any) {
	r.Add(Issue{Severity: SeverityWarn, Rule: rule, File: file, Message: fmt.Sprintf(format, args...)})
}

// Infof is sugar for Add(info severity).
func (r *Report) Infof(rule, file, format string, args ...any) {
	r.Add(Issue{Severity: SeverityInfo, Rule: rule, File: file, Message: fmt.Sprintf(format, args...)})
}

// HasErrors reports whether any error-severity issues were collected.
func (r *Report) HasErrors() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Counts returns (errors, warnings, infos).
func (r *Report) Counts() (int, int, int) {
	var e, w, n int
	for _, i := range r.Issues {
		switch i.Severity {
		case SeverityError:
			e++
		case SeverityWarn:
			w++
		case SeverityInfo:
			n++
		}
	}
	return e, w, n
}

// String renders the report as a human-readable string. Issues are grouped
// by file and sorted by severity-desc, then rule.
func (r *Report) String() string {
	if len(r.Issues) == 0 {
		return ""
	}
	byFile := map[string][]Issue{}
	files := []string{}
	for _, i := range r.Issues {
		f := i.File
		if f == "" {
			f = "<event>"
		}
		if _, ok := byFile[f]; !ok {
			files = append(files, f)
		}
		byFile[f] = append(byFile[f], i)
	}
	sort.Strings(files)

	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s\n", f)
		issues := byFile[f]
		sort.Slice(issues, func(i, j int) bool {
			if issues[i].Severity != issues[j].Severity {
				return issues[i].Severity > issues[j].Severity
			}
			return issues[i].Rule < issues[j].Rule
		})
		for _, i := range issues {
			fmt.Fprintf(&b, "  [%s %s] %s\n", strings.ToUpper(i.Severity.String()), i.Rule, i.Message)
		}
	}
	e, w, n := r.Counts()
	fmt.Fprintf(&b, "\n%d error(s), %d warning(s), %d info\n", e, w, n)
	return b.String()
}
