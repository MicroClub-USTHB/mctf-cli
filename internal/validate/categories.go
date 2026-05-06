package validate

import "github.com/MicroClub-USTHB/mctf-cli/internal/config"

// CoreCategories is the always-on category catalog (10 categories).
var CoreCategories = map[string]CategoryInfo{
	"web":       {Name: "Web Exploitation", Deployable: true, Range: PortRange{10000, 10999}},
	"pwn":       {Name: "Binary Exploitation", Deployable: true, Range: PortRange{11000, 11999}},
	"crypto":    {Name: "Cryptography", Deployable: false, Range: PortRange{12000, 12999}},
	"ai":        {Name: "AI / Prompt", Deployable: false, Range: PortRange{13000, 13999}},
	"misc":      {Name: "Miscellaneous", Deployable: false, Range: PortRange{14000, 14999}},
	"reverse":   {Name: "Reverse Engineering", Deployable: false, Range: PortRange{15000, 15999}},
	"jailbreak": {Name: "Sandbox Escape", Deployable: true, Range: PortRange{16000, 16999}},
	"forensics": {Name: "Digital Forensics", Deployable: false, Range: PortRange{0, 0}},
	"osint":     {Name: "Open-Source Intelligence", Deployable: false, Range: PortRange{0, 0}},
	"hardware":  {Name: "Hardware / Embedded", Deployable: false, Range: PortRange{17000, 17999}},
}

// ExtendedCategories is the opt-in catalog (12 categories).
// Each gets a 1000-port slice from 17000+ on activation, allocated in the
// declared order (slot index drives the offset).
var ExtendedCategories = []ExtendedCategory{
	{Slug: "stego", Name: "Steganography", Deployable: false},
	{Slug: "mobile", Name: "Mobile (Android/iOS)", Deployable: false},
	{Slug: "blockchain", Name: "Blockchain / Web3", Deployable: true},
	{Slug: "network", Name: "Network Recon / Forensics", Deployable: true},
	{Slug: "ppc", Name: "Programming / Algorithms", Deployable: true},
	{Slug: "realistic", Name: "Boot2Root / Pentest", Deployable: true},
	{Slug: "cloud", Name: "Cloud (AWS/GCP/Azure)", Deployable: true},
	{Slug: "iot", Name: "IoT / Embedded", Deployable: true},
	{Slug: "sysadmin", Name: "SysAdmin / Linux", Deployable: true},
	{Slug: "defensive", Name: "DFIR / Blue Team", Deployable: false},
	{Slug: "welcome", Name: "Welcome / Sanity", Deployable: false},
	{Slug: "quiz", Name: "Quiz / Trivia", Deployable: false},
}

// CategoryInfo holds the always-known facts about a category.
type CategoryInfo struct {
	Name       string
	Deployable bool
	Range      PortRange
}

// ExtendedCategory is one of the opt-in categories.
type ExtendedCategory struct {
	Slug       string
	Name       string
	Deployable bool
}

// PortRange is an inclusive range [Min, Max]. Zero-valued range means
// "no service ports allocated for this category" (e.g. forensics, osint).
type PortRange struct {
	Min int
	Max int
}

// Empty reports whether the range is unset.
func (r PortRange) Empty() bool { return r.Min == 0 && r.Max == 0 }

// Contains reports whether p is inside the range. Empty ranges contain nothing.
func (r PortRange) Contains(p int) bool {
	if r.Empty() {
		return false
	}
	return p >= r.Min && p <= r.Max
}

// ResolveCategories builds the active category catalog for an event,
// merging core + extended (from config) + custom (from config).
func ResolveCategories(c *config.Config) map[string]CategoryInfo {
	cats := make(map[string]CategoryInfo, len(CoreCategories))
	for k, v := range CoreCategories {
		cats[k] = v
	}
	if c == nil {
		return cats
	}
	// Extended: each declared slug gets the next slot in the 17000+ range.
	slot := 0
	for _, slug := range c.Categories.Extended {
		for _, e := range ExtendedCategories {
			if e.Slug == slug {
				cats[slug] = CategoryInfo{
					Name:       e.Name,
					Deployable: e.Deployable,
					Range:      PortRange{Min: 17000 + slot*1000, Max: 17000 + slot*1000 + 999},
				}
				slot++
				break
			}
		}
	}
	// Custom: explicit slug + name + optional port range.
	for _, cu := range c.Categories.Custom {
		info := CategoryInfo{Name: cu.Name, Deployable: cu.Deployable}
		if cu.PortRange != nil {
			info.Range = PortRange{Min: cu.PortRange.Min, Max: cu.PortRange.Max}
		}
		cats[cu.Slug] = info
	}
	return cats
}
