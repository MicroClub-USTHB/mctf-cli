package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Vulnerability represents one vulnerability reported by Trivy.
type Vulnerability struct {
	ID       string `json:"VulnerabilityID"`
	Severity string `json:"Severity"`
	PkgName  string `json:"PkgName"`
	Title    string `json:"Title"`
}

// trivyReport is the portion of Trivy's JSON output we need.
type trivyReport struct {
	Results []struct {
		Target          string          `json:"Target"`
		Vulnerabilities []Vulnerability `json:"Vulnerabilities"`
	} `json:"Results"`
}

// ParseTrivyJSON parses Trivy JSON and returns all reported vulnerabilities.
func ParseTrivyJSON(data []byte) ([]Vulnerability, error) {
	var report trivyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse Trivy JSON: %w", err)
	}

	var findings []Vulnerability
	for _, result := range report.Results {
		findings = append(findings, result.Vulnerabilities...)
	}
	return findings, nil
}

// ParseSeverityThreshold validates a comma-separated list of severity levels.
func ParseSeverityThreshold(value string) ([]string, error) {
	ranks := map[string]int{
		"UNKNOWN":  0,
		"LOW":      1,
		"MEDIUM":   2,
		"HIGH":     3,
		"CRITICAL": 4,
	}

	var levels []string
	seen := make(map[string]bool)

	for _, item := range strings.Split(value, ",") {
		level := strings.ToUpper(strings.TrimSpace(item))
		if level == "" {
			return nil, fmt.Errorf("severity threshold contains an empty value")
		}
		if _, ok := ranks[level]; !ok {
			return nil, fmt.Errorf("invalid severity %q; use UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL", level)
		}
		if !seen[level] {
			levels = append(levels, level)
			seen[level] = true
		}
	}

	if len(levels) == 0 {
		return nil, fmt.Errorf("severity threshold cannot be empty")
	}
	return levels, nil
}

// FindingsAtThreshold returns vulnerabilities matching one of the configured
// severity levels or any higher severity. For example, HIGH includes HIGH
// and CRITICAL findings.
func FindingsAtThreshold(findings []Vulnerability, threshold []string) []Vulnerability {
	ranks := map[string]int{
		"UNKNOWN":  0,
		"LOW":      1,
		"MEDIUM":   2,
		"HIGH":     3,
		"CRITICAL": 4,
	}

	minRank := 5
	for _, level := range threshold {
		if rank, ok := ranks[strings.ToUpper(level)]; ok && rank < minRank {
			minRank = rank
		}
	}

	var matches []Vulnerability
	for _, finding := range findings {
		if ranks[strings.ToUpper(finding.Severity)] >= minRank {
			matches = append(matches, finding)
		}
	}
	return matches
}

// ScanVulnerabilities runs Trivy against an image. The bool indicates whether
// scanning was performed; false means Trivy is not installed.
func ScanVulnerabilities(
	ctx context.Context,
	image string,
	threshold []string,
) ([]Vulnerability, bool, error) {
	trivy, err := exec.LookPath("trivy")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("find trivy: %w", err)
	}

	return scanVulnerabilitiesWithPath(ctx, trivy, image, threshold)
}

func scanVulnerabilitiesWithPath(
	ctx context.Context,
	trivy string,
	image string,
	threshold []string,
) ([]Vulnerability, bool, error) {
	cmd := exec.CommandContext(ctx, trivy, "image", "--format", "json", image)
	output, err := cmd.Output()
	if err != nil {
		return nil, true, fmt.Errorf("trivy scan %s: %w", image, err)
	}

	findings, err := ParseTrivyJSON(output)
	if err != nil {
		return nil, true, err
	}

	return FindingsAtThreshold(findings, threshold), true, nil
}
