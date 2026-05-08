package build

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

// makeTar builds an in-memory tar archive from the given (path, body)
// pairs. Used by every scan test below.
func makeTar(entries map[string]string) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for path, body := range entries {
		_ = tw.WriteHeader(&tar.Header{
			Name:     path,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	return buf.Bytes()
}

func TestScanReader_CleanImage(t *testing.T) {
	body := makeTar(map[string]string{
		"app/main.py":  "print('hello')",
		"etc/hostname": "container",
	})
	hits, err := ScanReader(bytes.NewReader(body), []string{"mctf{leak}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("clean image should have no hits, got %+v", hits)
	}
}

func TestScanReader_FindsFlag(t *testing.T) {
	body := makeTar(map[string]string{
		"app/main.py": "print('mctf{leak}')",
		"app/safe.py": "print('hello')",
	})
	hits, err := ScanReader(bytes.NewReader(body), []string{"mctf{leak}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %+v", hits)
	}
	if hits[0].Path != "app/main.py" || hits[0].Flag != "mctf{leak}" {
		t.Errorf("hit = %+v", hits[0])
	}
}

func TestScanReader_ExemptFiles(t *testing.T) {
	body := makeTar(map[string]string{
		"flag.txt":    "mctf{leak}",
		"app/FLAG":    "mctf{leak}",
		"app/x.flag":  "mctf{leak}",
		"app/main.py": "import flag\nprint('hello')",
	})
	hits, err := ScanReader(bytes.NewReader(body), []string{"mctf{leak}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("exempt files should not produce hits: %+v", hits)
	}
}

func TestScanReader_SkipsLargeFiles(t *testing.T) {
	big := strings.Repeat("x", MaxScanFileSize+1) + "mctf{leak}"
	body := makeTar(map[string]string{
		"app/big.bin": big,
	})
	hits, err := ScanReader(bytes.NewReader(body), []string{"mctf{leak}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("oversized file should be skipped, got %+v", hits)
	}
}

func TestScanReader_MultipleFlags(t *testing.T) {
	body := makeTar(map[string]string{
		"a.txt": "mctf{first}",
		"b.txt": "mctf{second}",
	})
	hits, err := ScanReader(bytes.NewReader(body), []string{"mctf{first}", "mctf{second}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits across 2 flags, got %+v", hits)
	}
}

func TestScanReader_EmptyFlagsNoOp(t *testing.T) {
	body := makeTar(map[string]string{
		"app/x.py": "anything goes",
	})
	hits, err := ScanReader(bytes.NewReader(body), []string{"", "  "})
	if err != nil {
		t.Fatal(err)
	}
	if hits != nil {
		t.Errorf("empty/whitespace flags should produce no scan: %+v", hits)
	}
}

func TestIsExempt(t *testing.T) {
	cases := map[string]bool{
		"flag.txt":        true,
		"app/flag.txt":    true,
		"FLAG":            true,
		"deep/path/FLAG":  true,
		"x.flag":          true,
		"app/secret.flag": true,
		"flag":            true,
		"main.py":         false,
		"flag-not-exempt": false,
	}
	for in, want := range cases {
		if got := isExempt(in); got != want {
			t.Errorf("isExempt(%q) = %v, want %v", in, got, want)
		}
	}
}
