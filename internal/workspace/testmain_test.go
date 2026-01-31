package workspace

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	tempHome, err := os.MkdirTemp("", "schmux-workspace-tests-home")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempHome)

	if err := os.Setenv("HOME", tempHome); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
