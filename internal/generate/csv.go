package generate

import (
	"bytes"
	"encoding/csv"
	"strconv"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

// csvHeader is locked at v1; new columns get appended on the right.
var csvHeader = []string{
	"category", "slug", "name", "author", "difficulty", "wave",
	"deploy", "dynamic", "archetype",
	"flag_count", "has_writeup", "has_hints", "asset_count",
}

// CSV renders the event-level tracking sheet.
func CSV(challs []*challenge.Challenge) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}
	for _, c := range challs {
		m := c.Manifest
		hasHints := len(m.Challenge.Hints) > 0 ||
			fileExists(c.SolutionDir + "/hints.md")
		row := []string{
			m.Challenge.Category,
			c.Slug,
			m.Challenge.Name,
			m.Challenge.Author,
			m.Challenge.Difficulty,
			strconv.Itoa(m.Challenge.Wave),
			boolStr(m.Challenge.NeedDeploy),
			boolStr(m.Deploy != nil && m.Deploy.DynamicInstances),
			string(c.Archetype),
			strconv.Itoa(len(m.Challenge.Flags)),
			boolStr(c.HasWriteup()),
			boolStr(hasHints),
			strconv.Itoa(len(listAssets(c))),
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
