package mdedit

import "testing"

func TestMerge(t *testing.T) {
	tests := []struct {
		name        string
		base, draft string
		disk        string
		want        string
		wantDropped int
	}{
		{"browser unchanged, disk wins", "x\n", "x\n", "y\n", "y\n", 0},
		{"no agent change", "a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", "a\nB\nc\n", 0},
		{"different lines", "a\nb\nc\n", "A\nb\nc\n", "a\nb\nC\n", "A\nb\nC\n", 0},
		{"same line, different spots",
			"The cat sat on the mat.\n", "The big cat sat on the mat.\n", "The cat sat on the rug.\n",
			"The big cat sat on the rug.\n", 0},
		{"identical replacement on both sides", "one\ntwo\nthree\n", "one\n2\nthree\n", "one\n2\nthree\n", "one\n2\nthree\n", 0},
		{"agent inserted lines above", "alpha\nbeta\ngamma\n", "alpha\nbeta!\ngamma\n", "intro\nintro2\nalpha\nbeta\ngamma\n",
			"intro\nintro2\nalpha\nbeta!\ngamma\n", 0},
		{"agent rewrote the region, fuzzy match lands the edit",
			"# T\n\nold paragraph text here\n", "# T\n\nold paragraph text here, plus\n", "# T\n\nCompletely different content now\n",
			"# T\n\nCompletely different content now, plus\n", 0},
		{"insertion already present is applied again (why save ids exist)",
			"# Title\n\nThe cat sat on the mat.\n\nSecond paragraph here.\n",
			"# Title\n\nThe big cat sat on the mat.\n\nSecond paragraph here.\n",
			"# Title\n\nThe big cat sat on the mat.\n\nSecond paragraph here, edited by agent.\n",
			"# Title\n\nThe big big cat sat on the mat.\n\nSecond paragraph here, edited by agent.\n", 0},
		{"no context anywhere: hunk dropped, disk unchanged",
			"aaaa bbbb cccc dddd\n", "aaaa bbbb XXXX cccc dddd\n", "completely unrelated text of a different nature\n",
			"completely unrelated text of a different nature\n", 1},
		{"empty base", "", "hello\n", "agent\n", "hello\nagent\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped := Merge(tt.base, tt.draft, tt.disk)
			if got != tt.want {
				t.Fatalf("Merge() = %q, want %q", got, tt.want)
			}
			if dropped != tt.wantDropped {
				t.Fatalf("dropped = %d, want %d", dropped, tt.wantDropped)
			}
		})
	}
}
