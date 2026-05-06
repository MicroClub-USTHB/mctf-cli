package challenge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Discover walks challengesDir and returns every challenge found, in stable
// path-sorted order. challengesDir is relative to eventRoot (or absolute).
//
// A challenge is identified by the existence of <root>/challenge/challenge.yml.
// Challenges that fail to parse are still returned in the result (as Challenge
// with a zero-value Manifest) so callers can surface them with proper error
// reporting; the second return value collects load errors per path.
func Discover(eventRoot, challengesDir string) ([]*Challenge, map[string]error, error) {
	root := challengesDir
	if !filepath.IsAbs(root) {
		root = filepath.Join(eventRoot, challengesDir)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, fmt.Errorf("challenges_dir %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("challenges_dir %q is not a directory", root)
	}

	var (
		out  []*Challenge
		errs = make(map[string]error)
	)

	categories, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	for _, cat := range categories {
		if !cat.IsDir() {
			continue
		}
		catDir := filepath.Join(root, cat.Name())
		challs, err := os.ReadDir(catDir)
		if err != nil {
			return nil, nil, err
		}
		for _, ch := range challs {
			if !ch.IsDir() {
				continue
			}
			chRoot := filepath.Join(catDir, ch.Name())
			chDir := filepath.Join(chRoot, "challenge")
			manifestPath := filepath.Join(chDir, "challenge.yml")
			if _, err := os.Stat(manifestPath); err != nil {
				// Not a challenge dir (no challenge/challenge.yml). Skip silently.
				continue
			}
			c := &Challenge{
				Slug:         ch.Name(),
				CategoryDir:  cat.Name(),
				RootDir:      chRoot,
				ChallengeDir: chDir,
				SolutionDir:  filepath.Join(chRoot, "solution"),
				AssetsDir:    filepath.Join(chRoot, "assets"),
				OtherDir:     filepath.Join(chRoot, "other"),
			}
			m, _, err := LoadManifest(manifestPath)
			if err != nil {
				errs[manifestPath] = err
			} else {
				c.Manifest = m
				c.Archetype = DeriveArchetype(m)
			}
			out = append(out, c)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CategoryDir != out[j].CategoryDir {
			return out[i].CategoryDir < out[j].CategoryDir
		}
		return out[i].Slug < out[j].Slug
	})
	return out, errs, nil
}
