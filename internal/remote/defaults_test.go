package remote

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type fakeRemoteClient struct {
	setOptCalls       [][2]string
	setServerOptCalls [][2]string
	execCalls         []string
}

func (f *fakeRemoteClient) SetOption(_ context.Context, opt, val string) error {
	f.setOptCalls = append(f.setOptCalls, [2]string{opt, val})
	return nil
}
func (f *fakeRemoteClient) SetServerOption(_ context.Context, opt, val string) error {
	f.setServerOptCalls = append(f.setServerOptCalls, [2]string{opt, val})
	return nil
}
func (f *fakeRemoteClient) Execute(_ context.Context, cmd string) (string, time.Duration, error) {
	f.execCalls = append(f.execCalls, cmd)
	return "", 0, nil
}

func TestApplyRemoteTmuxDefaults(t *testing.T) {
	f := &fakeRemoteClient{}
	applyRemoteTmuxDefaults(context.Background(), f, nil)

	wantServer := [][2]string{
		{"set-clipboard", "external"},
		{"terminal-features", "*:clipboard"},
	}
	if !reflect.DeepEqual(f.setServerOptCalls, wantServer) {
		t.Errorf("server-option calls = %v, want %v", f.setServerOptCalls, wantServer)
	}
	wantSession := [][2]string{{"window-size", "manual"}}
	if !reflect.DeepEqual(f.setOptCalls, wantSession) {
		t.Errorf("session-option calls = %v, want %v", f.setOptCalls, wantSession)
	}
	wantExec := []string{"setenv -g DISPLAY :99"}
	if !reflect.DeepEqual(f.execCalls, wantExec) {
		t.Errorf("execute calls = %v, want %v", f.execCalls, wantExec)
	}
}
