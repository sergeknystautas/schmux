//go:build !nomodelregistry

package models

import "testing"

func TestHasTextOutput(t *testing.T) {
	tests := []struct {
		name   string
		output []string
		want   bool
	}{
		{"text only", []string{"text"}, true},
		{"text among others", []string{"image", "text", "audio"}, true},
		{"no text", []string{"image", "audio"}, false},
		{"empty output", []string{}, false},
		{"nil output", nil, false},
		{"embedding only", []string{"embedding"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := registryModality{Output: tt.output}
			if got := hasTextOutput(m); got != tt.want {
				t.Errorf("hasTextOutput(%v) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestIsAllDigits(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"all digits", "20250514", true},
		{"single digit", "0", true},
		{"empty string", "", true}, // vacuously true per implementation
		{"with letters", "2025abc", false},
		{"with dash", "2025-05", false},
		{"with space", "2025 05", false},
		{"leading zero", "00000001", true},
		{"unicode digit", "٤", false}, // Arabic-Indic digit, not ASCII
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAllDigits(tt.input); got != tt.want {
				t.Errorf("isAllDigits(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
