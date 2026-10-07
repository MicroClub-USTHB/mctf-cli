package validate

import (
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

func TestU003_DynamicFlagsIgnored(t *testing.T) {
	c1 := &challenge.Challenge{
		CategoryDir: "web",
		Slug:        "dyn1",
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{
				Name: "dyn1",
				Flags: []challenge.Flag{
					{Dynamic: true, Flag: ""},
				},
			},
		},
	}
	c2 := &challenge.Challenge{
		CategoryDir: "pwn",
		Slug:        "dyn2",
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{
				Name: "dyn2",
				Flags: []challenge.Flag{
					{Dynamic: true, Flag: ""},
				},
			},
		},
	}

	r := &Report{}
	checkUniqueness(r, []*challenge.Challenge{c1, c2})

	if hasRule(r, "U003", SeverityError) {
		t.Fatalf("expected U003 to ignore dynamic flags with empty values, got: %s", r.String())
	}
}

func TestU003_StaticDuplicateFlagDetected(t *testing.T) {
	c1 := &challenge.Challenge{
		CategoryDir: "web",
		Slug:        "static1",
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{
				Name: "static1",
				Flags: []challenge.Flag{
					{Flag: "mctf{duplicate}"},
				},
			},
		},
	}
	c2 := &challenge.Challenge{
		CategoryDir: "crypto",
		Slug:        "static2",
		Manifest: challenge.Manifest{
			Challenge: challenge.ChallengeBlock{
				Name: "static2",
				Flags: []challenge.Flag{
					{Flag: "mctf{duplicate}"},
				},
			},
		},
	}

	r := &Report{}
	checkUniqueness(r, []*challenge.Challenge{c1, c2})

	if !hasRule(r, "U003", SeverityError) {
		t.Fatalf("expected U003 for duplicate static flag, got: %s", r.String())
	}
}
