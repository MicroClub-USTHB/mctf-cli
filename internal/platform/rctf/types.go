package rctf

// Challenge represents an rCTF challenge object as returned and accepted
// by the rCTF REST API (/api/v1/challs).
type Challenge struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Author           string          `json:"author,omitempty"`
	Description      string          `json:"description"`
	Category         string          `json:"category"`
	TiebreakEligible bool            `json:"tiebreakEligible"`
	SortWeight       int             `json:"sortWeight,omitempty"`
	Initial          int             `json:"initial,omitempty"`
	Min              int             `json:"min,omitempty"`
	Decay            int             `json:"decay,omitempty"`
	Files            []ChallengeFile `json:"files,omitempty"`
	Flags            []ChallengeFlag `json:"flags,omitempty"`
}

// ChallengeFile is a downloadable asset link attached to an rCTF challenge.
type ChallengeFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ChallengeFlag is a flag attached to an rCTF challenge.
type ChallengeFlag struct {
	Flag string `json:"flag"`
}

// Response is the standard envelope rCTF returns for API calls.
type Response[T any] struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
}

// ConfigMeta represents event metadata in rCTF.
type ConfigMeta struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	StartTime   *int64 `json:"startTime,omitempty"` // Unix milliseconds
	EndTime     *int64 `json:"endTime,omitempty"`   // Unix milliseconds
}

// AdminConfigUpdate is the payload sent to PATCH /api/v1/admin/config.
type AdminConfigUpdate struct {
	Meta ConfigMeta `json:"meta"`
}
