package build

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestParseTrivyJSON(t *testing.T) {
	input := []byte(`{
		"Results": [
			{
				"Target": "alpine:3.12",
				"Vulnerabilities": [
					{
						"VulnerabilityID": "CVE-2021-1234",
						"Severity": "CRITICAL",
						"PkgName": "openssl",
						"Title": "Example critical vulnerability"
					},
					{
						"VulnerabilityID": "CVE-2021-5678",
						"Severity": "LOW",
						"PkgName": "busybox",
						"Title": "Example low vulnerability"
					}
				]
			}
		]
	}`)

	got, err := ParseTrivyJSON(input)
	if err != nil {
		t.Fatalf("ParseTrivyJSON() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	if got[0].ID != "CVE-2021-1234" || got[0].Severity != "CRITICAL" {
		t.Errorf("unexpected first finding: %+v", got[0])
	}
}

func TestParseTrivyJSONInvalid(t *testing.T) {
	if _, err := ParseTrivyJSON([]byte(`not JSON`)); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestParseSeverityThreshold(t *testing.T) {
	got, err := ParseSeverityThreshold(" high, CRITICAL,high ")
	if err != nil {
		t.Fatalf("ParseSeverityThreshold() error = %v", err)
	}

	want := []string{"HIGH", "CRITICAL"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseSeverityThresholdInvalid(t *testing.T) {
	for _, input := range []string{"", "HIGH,", "URGENT", ",CRITICAL"} {
		if _, err := ParseSeverityThreshold(input); err == nil {
			t.Errorf("ParseSeverityThreshold(%q) expected an error", input)
		}
	}
}

func TestFindingsAtThreshold(t *testing.T) {
	findings := []Vulnerability{
		{ID: "CVE-1", Severity: "LOW"},
		{ID: "CVE-2", Severity: "MEDIUM"},
		{ID: "CVE-3", Severity: "HIGH"},
		{ID: "CVE-4", Severity: "CRITICAL"},
	}

	got := FindingsAtThreshold(findings, []string{"HIGH"})
	var ids []string
	for _, finding := range got {
		ids = append(ids, finding.ID)
	}

	want := []string{"CVE-3", "CVE-4"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("got %v, want %v", ids, want)
	}
}
func TestScanVulnerabilitiesMissingExecutable(t *testing.T) {
	_, scanned, err := scanVulnerabilitiesWithPath(
		context.Background(),
		"definitely-not-a-real-trivy-executable",
		"example:latest",
		[]string{"HIGH"},
	)

	if err == nil {
		t.Fatal("expected an error when the Trivy executable cannot run")
	}
	if !scanned {
		t.Fatal("expected scanned to be true when an executable path was supplied")
	}

	var exitErr *exec.Error
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an executable error, got %T: %v", err, err)
	}
}
