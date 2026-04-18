package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShellCommandRejectsLegacyStringForm(t *testing.T) {
	var sc ShellCommand
	err := json.Unmarshal([]byte(`"vcs-clone X Y"`), &sc)
	if err == nil {
		t.Fatal("expected legacy string-form to be rejected")
	}
	if !strings.Contains(err.Error(), "schmux config migrate") {
		t.Errorf("error doesn't mention migration tool: %v", err)
	}
}

func TestShellCommandAcceptsArgvForm(t *testing.T) {
	var sc ShellCommand
	if err := json.Unmarshal([]byte(`["vcs-clone","X","Y"]`), &sc); err != nil {
		t.Fatalf("argv form rejected: %v", err)
	}
	if len(sc) != 3 || sc[0] != "vcs-clone" {
		t.Errorf("got %v, want [vcs-clone X Y]", sc)
	}
}

func TestShellCommandRejectsOtherJSONShapes(t *testing.T) {
	cases := []string{
		`123`,
		`true`,
		`{"foo":"bar"}`,
		`[1, 2, 3]`,
	}
	for _, in := range cases {
		var sc ShellCommand
		if err := json.Unmarshal([]byte(in), &sc); err == nil {
			t.Errorf("input %s unexpectedly accepted", in)
		}
	}
}

func TestShellCommandRoundTripJSON(t *testing.T) {
	original := ShellCommand{"sh", "-c", "echo $1", "_", "{{.X}}"}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded ShellCommand
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("element %d: got %q, want %q", i, decoded[i], original[i])
		}
	}
}

func TestShellCommandNilMarshalsAsNull(t *testing.T) {
	var sc ShellCommand
	out, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(out) != "null" {
		t.Errorf("got %s, want null", out)
	}
}
