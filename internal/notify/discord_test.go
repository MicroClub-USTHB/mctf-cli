package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscord_Disabled_NoOps(t *testing.T) {
	d := NewDiscord("")
	if !d.Disabled() {
		t.Error("empty URL should be disabled")
	}
	if err := d.Notify(context.Background(), Message{Title: "x"}); err != nil {
		t.Errorf("disabled notifier should no-op without error, got %v", err)
	}
}

func TestDiscord_PostsEmbed(t *testing.T) {
	var got struct {
		Username string           `json:"username"`
		Embeds   []map[string]any `json:"embeds"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	d := NewDiscord(srv.URL)
	if err := d.Notify(context.Background(), Message{
		Title: "Deploy succeeded",
		Body:  "5 challenges up",
		Level: "success",
	}); err != nil {
		t.Fatal(err)
	}
	if got.Username != "MCTF" {
		t.Errorf("username = %q", got.Username)
	}
	if len(got.Embeds) != 1 {
		t.Fatalf("want 1 embed, got %d", len(got.Embeds))
	}
	e := got.Embeds[0]
	if e["title"] != "Deploy succeeded" || e["description"] != "5 challenges up" {
		t.Errorf("embed = %+v", e)
	}
	// Color is encoded as JSON number — check it's the green for success.
	if got, want := int(e["color"].(float64)), 0x2ecc71; got != want {
		t.Errorf("color = %#x, want %#x", got, want)
	}
}

func TestDiscord_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	d := NewDiscord(srv.URL)
	err := d.Notify(context.Background(), Message{Title: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("want 429 error, got %v", err)
	}
}

func TestColorFor_KnownLevels(t *testing.T) {
	cases := map[string]int{
		"info":    0x3498db,
		"success": 0x2ecc71,
		"warn":    0xf1c40f,
		"error":   0xe74c3c,
		"":        0x3498db,
		"weird":   0x3498db,
	}
	for level, want := range cases {
		if got := colorFor(level); got != want {
			t.Errorf("colorFor(%q) = %#x, want %#x", level, got, want)
		}
	}
}
