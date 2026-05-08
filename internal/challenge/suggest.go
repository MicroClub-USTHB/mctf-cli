package challenge

import (
	"sort"
	"strings"
)

// Suggest returns up to 3 challenge keys ("<cat>/<slug>") that look like
// what the user probably meant by query. The match is two-pass:
//
//  1. substring / prefix on the slug (or the full key if query has a slash);
//  2. Levenshtein distance ≤ 2 on the slug, as a fallback.
//
// Empty result means no near-match — the caller should fall back to
// listing every known challenge.
func Suggest(challs []*Challenge, query string) []string {
	if len(challs) == 0 || query == "" {
		return nil
	}
	qCat, qSlug := splitQuery(query)
	qSlug = strings.ToLower(qSlug)
	qCat = strings.ToLower(qCat)

	// Gate by category only when the supplied category matches a real
	// directory — that lets a category typo like "crpto/rsa-baby" still
	// surface "crypto/rsa-baby" via the slug match.
	gateCat := false
	if qCat != "" {
		for _, c := range challs {
			if strings.EqualFold(c.CategoryDir, qCat) {
				gateCat = true
				break
			}
		}
	}

	type scored struct {
		key   string
		score int // lower is better
	}
	var hits []scored

	for _, c := range challs {
		key := c.CategoryDir + "/" + c.Slug
		slug := strings.ToLower(c.Slug)
		cat := strings.ToLower(c.CategoryDir)

		if gateCat && qCat != cat {
			continue
		}

		// Skip the exact "<cat>/<slug>" match — the caller would have
		// already resolved it. (An exact slug match in a *different*
		// category, e.g. when the user typoed the category, is still a
		// useful suggestion.)
		if cat == qCat && slug == qSlug {
			continue
		}
		switch {
		case slug == qSlug:
			hits = append(hits, scored{key, 0})
		case strings.HasPrefix(slug, qSlug):
			hits = append(hits, scored{key, 1})
		case strings.Contains(slug, qSlug):
			hits = append(hits, scored{key, 2})
		default:
			d := levenshtein(qSlug, slug)
			if d <= 2 {
				hits = append(hits, scored{key, 10 + d})
			}
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return hits[i].key < hits[j].key
	})

	if len(hits) > 3 {
		hits = hits[:3]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.key
	}
	return out
}

// splitQuery accepts "<cat>/<slug>" or just "<slug>".
func splitQuery(q string) (cat, slug string) {
	if i := strings.IndexByte(q, '/'); i >= 0 {
		return q[:i], q[i+1:]
	}
	return "", q
}

// levenshtein returns the edit distance between a and b. Iterative,
// two-row DP; sufficient for short slug strings.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
