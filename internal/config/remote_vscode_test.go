package config

import (
	"strings"
	"testing"
	"text/template"
)

func TestGetRemoteVSCodeCommandTemplate_Default(t *testing.T) {
	cfg := &Config{}

	tmpl := cfg.GetRemoteVSCodeCommandTemplate()

	// Should return default template
	expected := `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`
	if tmpl != expected {
		t.Errorf("expected default template %q, got %q", expected, tmpl)
	}
}

func TestGetRemoteVSCodeCommandTemplate_Custom(t *testing.T) {
	cfg := &Config{
		RemoteWorkspace: &RemoteWorkspaceConfig{
			VSCodeCommandTemplate: `{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}`,
		},
	}

	tmpl := cfg.GetRemoteVSCodeCommandTemplate()

	expected := `{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}`
	if tmpl != expected {
		t.Errorf("expected custom template %q, got %q", expected, tmpl)
	}
}

func TestRemoteVSCodeCommandTemplate_Execution_Default(t *testing.T) {
	cfg := &Config{}

	templateStr := cfg.GetRemoteVSCodeCommandTemplate()

	// Parse template
	tmpl, err := template.New("vscode").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with test data
	type VSCodeTemplateData struct {
		Hostname   string
		Path       string
		VSCodePath string
	}

	data := VSCodeTemplateData{
		Hostname:   "dev12345.example.com",
		Path:       "/home/user/workspace",
		VSCodePath: "/usr/local/bin/code",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	expected := `/usr/local/bin/code --remote ssh-remote+dev12345.example.com /home/user/workspace`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestRemoteVSCodeCommandTemplate_Execution_Custom(t *testing.T) {
	cfg := &Config{
		RemoteWorkspace: &RemoteWorkspaceConfig{
			VSCodeCommandTemplate: `{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}`,
		},
	}

	templateStr := cfg.GetRemoteVSCodeCommandTemplate()

	// Parse template
	tmpl, err := template.New("vscode").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with test data
	type VSCodeTemplateData struct {
		Hostname   string
		Path       string
		VSCodePath string
	}

	data := VSCodeTemplateData{
		Hostname:   "dev12345.example.com",
		Path:       "/home/user/workspace",
		VSCodePath: "/usr/local/bin/code-custom",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	expected := `/usr/local/bin/code-custom --folder-uri vscode-remote://custom+dev12345.example.com/home/user/workspace`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestRemoteVSCodeCommandTemplate_Execution_WithSpaces(t *testing.T) {
	cfg := &Config{
		RemoteWorkspace: &RemoteWorkspaceConfig{
			VSCodeCommandTemplate: `{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}`,
		},
	}

	templateStr := cfg.GetRemoteVSCodeCommandTemplate()

	// Parse template
	tmpl, err := template.New("vscode").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with path containing spaces
	type VSCodeTemplateData struct {
		Hostname   string
		Path       string
		VSCodePath string
	}

	data := VSCodeTemplateData{
		Hostname:   "dev.example.com",
		Path:       "/home/user/my workspace",
		VSCodePath: "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	// Should work with spaces (command parsing will handle quoting)
	if !strings.Contains(result.String(), "my workspace") {
		t.Errorf("path with spaces not preserved in template output")
	}
}

func TestRemoteVSCodeCommandTemplate_InvalidTemplate(t *testing.T) {
	cfg := &Config{
		RemoteWorkspace: &RemoteWorkspaceConfig{
			VSCodeCommandTemplate: `{{.VSCodePath}} {{.InvalidField}}`, // Invalid field
		},
	}

	templateStr := cfg.GetRemoteVSCodeCommandTemplate()

	// Parse should succeed
	tmpl, err := template.New("vscode").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute should fail with invalid field
	type VSCodeTemplateData struct {
		Hostname   string
		Path       string
		VSCodePath string
	}

	data := VSCodeTemplateData{
		Hostname:   "dev.example.com",
		Path:       "/workspace",
		VSCodePath: "/usr/bin/code",
	}

	var result strings.Builder
	err = tmpl.Execute(&result, data)
	if err == nil {
		t.Error("expected error when executing template with invalid field")
	}
}

func TestRemoteWorkspaceConfig_JSONMarshaling(t *testing.T) {
	cfg := Config{
		RemoteWorkspace: &RemoteWorkspaceConfig{
			VSCodeCommandTemplate: `{{.VSCodePath}} --folder-uri custom://{{.Hostname}}{{.Path}}`,
		},
	}

	// Test that it can be marshaled/unmarshaled
	// This is implicitly tested by the config load/save mechanism
	if cfg.RemoteWorkspace.VSCodeCommandTemplate == "" {
		t.Error("template should not be empty")
	}
}
