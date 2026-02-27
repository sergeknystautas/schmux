package floormanager

import (
	"sort"
	"strings"
	"testing"
)

func TestMergeEnv(t *testing.T) {
	tests := []struct {
		name      string
		base      map[string]string
		overrides map[string]string
		want      map[string]string
	}{
		{
			name:      "both empty",
			base:      map[string]string{},
			overrides: map[string]string{},
			want:      map[string]string{},
		},
		{
			name:      "base only",
			base:      map[string]string{"A": "1", "B": "2"},
			overrides: map[string]string{},
			want:      map[string]string{"A": "1", "B": "2"},
		},
		{
			name:      "overrides only",
			base:      map[string]string{},
			overrides: map[string]string{"X": "10"},
			want:      map[string]string{"X": "10"},
		},
		{
			name:      "override replaces base",
			base:      map[string]string{"KEY": "old"},
			overrides: map[string]string{"KEY": "new"},
			want:      map[string]string{"KEY": "new"},
		},
		{
			name:      "non-overlapping keys merged",
			base:      map[string]string{"A": "1"},
			overrides: map[string]string{"B": "2"},
			want:      map[string]string{"A": "1", "B": "2"},
		},
		{
			name:      "nil base",
			base:      nil,
			overrides: map[string]string{"X": "1"},
			want:      map[string]string{"X": "1"},
		},
		{
			name:      "nil overrides",
			base:      map[string]string{"A": "1"},
			overrides: nil,
			want:      map[string]string{"A": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeEnv(tt.base, tt.overrides)
			if len(got) != len(tt.want) {
				t.Fatalf("mergeEnv() returned %d entries, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("mergeEnv()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestBuildEnvPrefix(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantEmpty bool
		wantSub   []string // substrings that must appear
	}{
		{
			name:      "empty map",
			env:       map[string]string{},
			wantEmpty: true,
		},
		{
			name:      "nil map",
			env:       nil,
			wantEmpty: true,
		},
		{
			name:    "single key-value",
			env:     map[string]string{"API_KEY": "secret"},
			wantSub: []string{"API_KEY='secret'"},
		},
		{
			name:    "value with spaces is quoted",
			env:     map[string]string{"MSG": "hello world"},
			wantSub: []string{"MSG='hello world'"},
		},
		{
			name:    "multiple keys all present",
			env:     map[string]string{"A": "1", "B": "2"},
			wantSub: []string{"A='1'", "B='2'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEnvPrefix(tt.env)
			if tt.wantEmpty && got != "" {
				t.Errorf("buildEnvPrefix() = %q, want empty", got)
				return
			}
			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("buildEnvPrefix() = %q, missing %q", got, sub)
				}
			}
		})
	}
}

func TestBuildEnvPrefix_Sorted(t *testing.T) {
	// buildEnvPrefix uses joinParts which doesn't sort, but let's verify
	// the output contains all keys (map iteration order is random).
	env := map[string]string{"Z": "1", "A": "2", "M": "3"}
	got := buildEnvPrefix(env)
	parts := strings.Fields(got)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %q", len(parts), got)
	}
	// Extract key names and verify all present
	var keys []string
	for _, p := range parts {
		k := strings.SplitN(p, "=", 2)[0]
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"A", "M", "Z"}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("sorted keys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestJoinParts(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "empty", parts: []string{}, want: ""},
		{name: "single", parts: []string{"hello"}, want: "hello"},
		{name: "two", parts: []string{"a", "b"}, want: "a b"},
		{name: "three", parts: []string{"x", "y", "z"}, want: "x y z"},
		{name: "nil", parts: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinParts(tt.parts)
			if got != tt.want {
				t.Errorf("joinParts(%v) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}
