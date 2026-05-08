package challenge

import (
	"reflect"
	"testing"
)

func mkChalls(keys ...string) []*Challenge {
	out := make([]*Challenge, 0, len(keys))
	for _, k := range keys {
		var cat, slug string
		for i, ch := range k {
			if ch == '/' {
				cat, slug = k[:i], k[i+1:]
				break
			}
		}
		out = append(out, &Challenge{CategoryDir: cat, Slug: slug})
	}
	return out
}

func TestSuggest_PrefixMatch(t *testing.T) {
	challs := mkChalls("crypto/rsa-baby", "crypto/rsa-padding", "web/cub3")
	got := Suggest(challs, "crypto/rs")
	want := []string{"crypto/rsa-baby", "crypto/rsa-padding"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSuggest_SubstringMatch(t *testing.T) {
	challs := mkChalls("web/sql-walkthrough", "web/cub3", "pwn/baby-overflow")
	got := Suggest(challs, "walk")
	want := []string{"web/sql-walkthrough"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSuggest_LevenshteinFallback(t *testing.T) {
	challs := mkChalls("crypto/rsa-baby", "web/cub3")
	got := Suggest(challs, "crypto/rsababy")
	want := []string{"crypto/rsa-baby"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSuggest_CategoryGate(t *testing.T) {
	challs := mkChalls("crypto/rsa-baby", "web/rsa-overlap")
	got := Suggest(challs, "crypto/rs")
	want := []string{"crypto/rsa-baby"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("category gate ignored: got %v, want %v", got, want)
	}
}

func TestSuggest_CapsAtThree(t *testing.T) {
	challs := mkChalls(
		"web/cub3-a", "web/cub3-b", "web/cub3-c", "web/cub3-d",
	)
	got := Suggest(challs, "web/cub3")
	if len(got) != 3 {
		t.Errorf("expected 3 suggestions, got %d (%v)", len(got), got)
	}
}

func TestSuggest_NoNearMatchReturnsEmpty(t *testing.T) {
	challs := mkChalls("web/cub3", "pwn/baby")
	got := Suggest(challs, "crypto/totally-unrelated-thing")
	if len(got) != 0 {
		t.Errorf("expected no suggestions, got %v", got)
	}
}

// When the user typos the category (no real "crpto" dir exists), the
// slug match should still surface even though the category gate would
// otherwise filter it out.
func TestSuggest_CategoryTypoFallsBackToSlugMatch(t *testing.T) {
	challs := mkChalls("crypto/rsa-baby", "web/cub3")
	got := Suggest(challs, "crpto/rsa-baby")
	want := []string{"crypto/rsa-baby"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"rsa-baby", "rsababy", 1},
	}
	for _, tc := range cases {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
