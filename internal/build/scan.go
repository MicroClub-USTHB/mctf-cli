package build

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ScanHit is one match of a flag value inside the image's filesystem.
type ScanHit struct {
	Path string // path inside the container
	Flag string // matched flag value
}

// MaxScanFileSize bounds per-file scan cost. Files larger than this
// are skipped (unlikely to be the place flags hide; binaries can run
// much bigger than the threshold).
const MaxScanFileSize = 8 * 1024 * 1024 // 8 MiB

// scanExempt are filenames inside the image that are *expected* to
// contain the flag (e.g. /flag.txt the challenge serves).
var scanExempt = []string{
	"flag.txt",
	"FLAG",
	"flag",
}

// ScanReader walks the tar stream from `docker export` and returns
// every (path, flag) pair where the file content contains one of the
// flag values. Pure — does not invoke docker.
//
// Returns the slice of hits and any decode error. An empty slice
// means the image is clean.
func ScanReader(r io.Reader, flags []string) ([]ScanHit, error) {
	var nonEmpty []string
	for _, f := range flags {
		if strings.TrimSpace(f) != "" {
			nonEmpty = append(nonEmpty, f)
		}
	}
	if len(nonEmpty) == 0 {
		return nil, nil
	}

	tr := tar.NewReader(r)
	var hits []ScanHit
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return hits, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Size <= 0 || hdr.Size > MaxScanFileSize {
			continue
		}
		if isExempt(hdr.Name) {
			continue
		}
		// Read into memory once; OK because we already capped Size.
		body, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
		if err != nil {
			return hits, fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}
		for _, flag := range nonEmpty {
			if bytes.Contains(body, []byte(flag)) {
				hits = append(hits, ScanHit{Path: hdr.Name, Flag: flag})
				break // one hit per file is enough
			}
		}
	}
	return hits, nil
}

// isExempt reports whether the filename is on the allow-list (i.e.
// the file is *expected* to contain the flag). Match by basename only
// so paths like /app/flag.txt are exempt regardless of where they
// land in the image.
func isExempt(p string) bool {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	for _, e := range scanExempt {
		if p == e {
			return true
		}
	}
	return strings.HasSuffix(p, ".flag")
}

// ScanImage runs `docker create <tag>` to produce an ephemeral
// container, streams `docker export <cid>` through ScanReader, and
// cleans up. The container is never started, so no challenge code
// runs during the scan.
//
// Returns hits (empty if clean) and any error. Cleanup is done via
// defer so a panic mid-scan still removes the container.
func ScanImage(ctx context.Context, tag string, flags []string) ([]ScanHit, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not in $PATH")
	}
	createOut, err := exec.CommandContext(ctx, "docker", "create", tag).Output()
	if err != nil {
		return nil, fmt.Errorf("docker create %s: %w", tag, err)
	}
	cid := strings.TrimSpace(string(createOut))
	if cid == "" {
		return nil, fmt.Errorf("docker create returned empty container id")
	}
	defer func() {
		_ = exec.Command("docker", "rm", "-f", cid).Run()
	}()

	export := exec.CommandContext(ctx, "docker", "export", cid)
	stdout, err := export.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("export pipe: %w", err)
	}
	if err := export.Start(); err != nil {
		return nil, fmt.Errorf("docker export start: %w", err)
	}

	hits, scanErr := ScanReader(stdout, flags)
	// Always wait for export to finish to reap the process.
	waitErr := export.Wait()
	if scanErr != nil {
		return hits, scanErr
	}
	if waitErr != nil {
		return hits, fmt.Errorf("docker export wait: %w", waitErr)
	}
	return hits, nil
}
