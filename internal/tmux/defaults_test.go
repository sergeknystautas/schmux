package tmux

import (
	"context"
	"testing"
)

type fakeOptionSetter struct {
	calls [][2]string // [opt, value]
}

func (f *fakeOptionSetter) SetServerOption(_ context.Context, opt, value string) error {
	f.calls = append(f.calls, [2]string{opt, value})
	return nil
}

func TestApplyTmuxServerDefaultsSetsExpectedOptions(t *testing.T) {
	f := &fakeOptionSetter{}
	ApplyTmuxServerDefaults(context.Background(), f, nil) // nil logger ok
	want := [][2]string{
		{"set-clipboard", "external"},
		{"terminal-features", "*:clipboard"},
	}
	if len(f.calls) != len(want) {
		t.Fatalf("got %d calls, want %d", len(f.calls), len(want))
	}
	for i, c := range f.calls {
		if c != want[i] {
			t.Errorf("call %d = %v, want %v", i, c, want[i])
		}
	}
}
