package instancer

import (
	"context"
	"errors"
	"testing"
)

func TestFakeRunner_RecordsCalls(t *testing.T) {
	f := NewFakeRunner()
	spec := SpawnSpec{ID: "a", ComposeFile: "/x.yml"}
	ids, err := f.Up(context.Background(), spec)
	if err != nil || len(ids) != 1 {
		t.Errorf("Up: ids=%v err=%v", ids, err)
	}
	if err := f.Down(context.Background(), spec, true); err != nil {
		t.Fatal(err)
	}
	if len(f.UpCalls) != 1 || f.UpCalls[0] != "a" {
		t.Errorf("UpCalls = %v", f.UpCalls)
	}
	if len(f.DownCalls) != 1 || f.DownCalls[0] != "a" {
		t.Errorf("DownCalls = %v", f.DownCalls)
	}
}

func TestFakeRunner_InjectsErrors(t *testing.T) {
	f := NewFakeRunner()
	f.ErrOnUp = map[string]error{"bad": errors.New("up nope")}
	f.ErrOnDown = map[string]error{"bad": errors.New("down nope")}
	if _, err := f.Up(context.Background(), SpawnSpec{ID: "bad"}); err == nil {
		t.Error("expected Up error")
	}
	if err := f.Down(context.Background(), SpawnSpec{ID: "bad"}, false); err == nil {
		t.Error("expected Down error")
	}
	// And they shouldn't have been recorded.
	if len(f.UpCalls) != 0 || len(f.DownCalls) != 0 {
		t.Errorf("error calls were recorded: %v %v", f.UpCalls, f.DownCalls)
	}
}
