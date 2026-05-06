package challenge

import (
	"path/filepath"
	"testing"
)

func TestDiscoverSampleEvent(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "sample-event")
	got, errs, err := Discover(root, "./challenges")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("unexpected per-manifest errors: %v", errs)
	}
	want := []struct {
		category, slug string
		archetype      Archetype
	}{
		{"crypto", "rsa-baby", ArchetypeStatic},
		{"forensics", "pcap-101", ArchetypeStatic},
		{"misc", "welcome", ArchetypeService},
		{"pwn", "baby-overflow", ArchetypeInstanced},
		{"web", "cub3-clone", ArchetypeInstanced},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d challenges, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].CategoryDir != w.category {
			t.Errorf("[%d] category = %q, want %q", i, got[i].CategoryDir, w.category)
		}
		if got[i].Slug != w.slug {
			t.Errorf("[%d] slug = %q, want %q", i, got[i].Slug, w.slug)
		}
		if got[i].Archetype != w.archetype {
			t.Errorf("[%d] archetype = %q, want %q", i, got[i].Archetype, w.archetype)
		}
	}
}
