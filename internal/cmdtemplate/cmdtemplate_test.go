package cmdtemplate

import "testing"

func TestRenderBasic(t *testing.T) {
	tmpl := Template{"echo", "{{.Name}}"}
	argv, err := tmpl.Render(map[string]string{"Name": "world"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"echo", "world"}
	if len(argv) != 2 || argv[0] != want[0] || argv[1] != want[1] {
		t.Errorf("got %v, want %v", argv, want)
	}
}

func TestRenderRejectsEmpty(t *testing.T) {
	if _, err := (Template{}).Render(nil); err == nil {
		t.Error("empty template accepted")
	}
}

func TestRenderRejectsEmptyRenderedSlot(t *testing.T) {
	tmpl := Template{"echo", "{{.X}}"}
	_, err := tmpl.Render(map[string]string{"X": ""})
	if err == nil {
		t.Error("empty rendered slot accepted")
	}
}

func TestRenderRejectsMissingVariable(t *testing.T) {
	tmpl := Template{"echo", "{{.Missing}}"}
	_, err := tmpl.Render(map[string]string{"Other": "x"})
	if err == nil {
		t.Error("missing variable accepted")
	}
}

func TestRenderPreservesShellMetacharsInValue(t *testing.T) {
	// Value containing ;,|,$,backtick, newline must remain a single argv element.
	tmpl := Template{"echo", "{{.X}}"}
	argv, err := tmpl.Render(map[string]string{"X": "a; rm -rf /; b\n`evil`$(evil)"})
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 2 {
		t.Errorf("metachar value split into %d elements, want 1", len(argv)-1)
	}
}

func TestRenderShellEscapeHatchAllowsLiteralScript(t *testing.T) {
	// argv[0] basename in {sh,bash,...} and argv[1] == "-c": script slot must be
	// literal, but positional args after can be templated.
	tmpl := Template{"sh", "-c", "echo $1", "_", "{{.X}}"}
	argv, err := tmpl.Render(map[string]string{"X": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 5 || argv[4] != "hello" {
		t.Errorf("got %v, want positional arg 'hello'", argv)
	}
}

func TestRenderShellEscapeHatchRejectsTemplateInScript(t *testing.T) {
	// Template syntax in the script slot is the bug class — must reject.
	tmpl := Template{"sh", "-c", "echo {{.X}}", "_", "literal"}
	_, err := tmpl.Render(map[string]string{"X": "anything"})
	if err == nil {
		t.Error("template syntax in shell script slot was accepted; should be rejected")
	}
}
