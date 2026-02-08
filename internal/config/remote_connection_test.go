package config

import (
	"strings"
	"testing"
	"text/template"
)

func TestRemoteFlavor_GetConnectCommandTemplate_Default(t *testing.T) {
	rf := RemoteFlavor{
		Flavor: "dev.example.com",
	}

	tmpl := rf.GetConnectCommandTemplate()

	// Should return default SSH template with tmux appended
	expected := `ssh {{.Flavor}} -- tmux -CC new-session -A -s schmux`
	if tmpl != expected {
		t.Errorf("expected default template %q, got %q", expected, tmpl)
	}
}

func TestRemoteFlavor_GetConnectCommandTemplate_Custom(t *testing.T) {
	rf := RemoteFlavor{
		Flavor:         "gpu-large",
		ConnectCommand: `cloud-ssh connect {{.Flavor}}`,
	}

	tmpl := rf.GetConnectCommandTemplate()

	// Should append tmux to custom command
	expected := `cloud-ssh connect {{.Flavor}} -- tmux -CC new-session -A -s schmux`
	if tmpl != expected {
		t.Errorf("expected custom template %q, got %q", expected, tmpl)
	}
}

func TestRemoteFlavor_GetReconnectCommandTemplate_Default(t *testing.T) {
	rf := RemoteFlavor{
		Flavor: "dev.example.com",
	}

	tmpl := rf.GetReconnectCommandTemplate()

	// Should return default SSH reconnect template with tmux appended
	expected := `ssh {{.Hostname}} -- tmux -CC new-session -A -s schmux`
	if tmpl != expected {
		t.Errorf("expected default reconnect template %q, got %q", expected, tmpl)
	}
}

func TestRemoteFlavor_GetReconnectCommandTemplate_Custom(t *testing.T) {
	rf := RemoteFlavor{
		Flavor:           "gpu-large",
		ReconnectCommand: `cloud-ssh reconnect {{.Hostname}}`,
	}

	tmpl := rf.GetReconnectCommandTemplate()

	// Should append tmux to custom reconnect command
	expected := `cloud-ssh reconnect {{.Hostname}} -- tmux -CC new-session -A -s schmux`
	if tmpl != expected {
		t.Errorf("expected custom reconnect template %q, got %q", expected, tmpl)
	}
}

func TestRemoteFlavor_GetReconnectCommandTemplate_FallbackToConnect(t *testing.T) {
	rf := RemoteFlavor{
		Flavor:         "gpu-large",
		ConnectCommand: `cloud-ssh connect {{.Flavor}}`,
		// No ReconnectCommand - should fall back to ConnectCommand
	}

	tmpl := rf.GetReconnectCommandTemplate()

	// Should use ConnectCommand as base and append tmux
	expected := `cloud-ssh connect {{.Flavor}} -- tmux -CC new-session -A -s schmux`
	if tmpl != expected {
		t.Errorf("expected fallback to connect template %q, got %q", expected, tmpl)
	}
}

func TestConnectCommandTemplate_Execution_SSH(t *testing.T) {
	rf := RemoteFlavor{
		Flavor: "dev12345.example.com",
	}

	templateStr := rf.GetConnectCommandTemplate()

	// Parse template
	tmpl, err := template.New("connect").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with test data
	type ConnectTemplateData struct {
		Flavor string
	}

	data := ConnectTemplateData{
		Flavor: "dev12345.example.com",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	expected := `ssh dev12345.example.com -- tmux -CC new-session -A -s schmux`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestConnectCommandTemplate_Execution_Custom(t *testing.T) {
	rf := RemoteFlavor{
		Flavor:         "gpu-large",
		ConnectCommand: `cloud-ssh connect {{.Flavor}}`,
	}

	templateStr := rf.GetConnectCommandTemplate()

	// Parse template
	tmpl, err := template.New("connect").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with test data
	type ConnectTemplateData struct {
		Flavor string
	}

	data := ConnectTemplateData{
		Flavor: "gpu-large",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	// Tmux parts should be appended automatically
	expected := `cloud-ssh connect gpu-large -- tmux -CC new-session -A -s schmux`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestReconnectCommandTemplate_Execution_SSH(t *testing.T) {
	rf := RemoteFlavor{
		Flavor: "dev.example.com",
	}

	templateStr := rf.GetReconnectCommandTemplate()

	// Parse template
	tmpl, err := template.New("reconnect").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with test data
	type ReconnectTemplateData struct {
		Hostname string
		Flavor   string
	}

	data := ReconnectTemplateData{
		Hostname: "dev12345.example.com",
		Flavor:   "dev.example.com",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	expected := `ssh dev12345.example.com -- tmux -CC new-session -A -s schmux`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestReconnectCommandTemplate_Execution_Custom(t *testing.T) {
	rf := RemoteFlavor{
		Flavor:           "gpu-large",
		ReconnectCommand: `cloud-ssh reconnect {{.Hostname}}`,
	}

	templateStr := rf.GetReconnectCommandTemplate()

	// Parse template
	tmpl, err := template.New("reconnect").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute template with test data
	type ReconnectTemplateData struct {
		Hostname string
		Flavor   string
	}

	data := ReconnectTemplateData{
		Hostname: "host123.example.com",
		Flavor:   "gpu-large",
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	// Tmux parts should be appended automatically
	expected := `cloud-ssh reconnect host123.example.com -- tmux -CC new-session -A -s schmux`
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestConnectCommandTemplate_InvalidTemplate(t *testing.T) {
	rf := RemoteFlavor{
		Flavor:         "test-flavor",
		ConnectCommand: `ssh {{.InvalidField}}`, // Invalid field
	}

	templateStr := rf.GetConnectCommandTemplate()

	// Parse should succeed
	tmpl, err := template.New("connect").Parse(templateStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Execute should fail with invalid field
	type ConnectTemplateData struct {
		Flavor string
	}

	data := ConnectTemplateData{
		Flavor: "test-flavor",
	}

	var result strings.Builder
	err = tmpl.Execute(&result, data)
	if err == nil {
		t.Error("expected error when executing template with invalid field")
	}
}
