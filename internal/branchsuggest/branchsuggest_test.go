package branchsuggest

import "testing"

func TestParseResult(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantOK       bool
		wantBranch   string
		wantNickname string
	}{
		{
			name:         "valid json",
			input:        `{"branch":"feature/test-branch","nickname":"Test branch"}`,
			wantOK:       true,
			wantBranch:   "feature/test-branch",
			wantNickname: "Test branch",
		},
		{
			name:         "fenced json",
			input:        "```json\n{\"branch\":\"fix/issue\",\"nickname\":\"Issue fix\"}\n```",
			wantOK:       true,
			wantBranch:   "fix/issue",
			wantNickname: "Issue fix",
		},
		{
			name:         "extra text around json",
			input:        "Here you go:\n{\"branch\":\"refactor/auth\",\"nickname\":\"Auth refactor\"}\nThanks.",
			wantOK:       true,
			wantBranch:   "refactor/auth",
			wantNickname: "Auth refactor",
		},
		{
			name:         "curly quotes normalize",
			input:        "{\u201cbranch\u201d: \u201cfeature/dark-mode\u201d, \u201cnickname\u201d: \u201cDark mode\u201d}",
			wantOK:       true,
			wantBranch:   "feature/dark-mode",
			wantNickname: "Dark mode",
		},
		{
			name:   "empty",
			input:  "",
			wantOK: false,
		},
		{
			name:   "no json",
			input:  "nope",
			wantOK: false,
		},
		{
			name:   "empty branch",
			input:  `{"branch":"  ","nickname":"Blank"}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResult(tt.input)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("expected ok, got error: %v", err)
				}
				if got.Branch != tt.wantBranch {
					t.Fatalf("branch = %q, want %q", got.Branch, tt.wantBranch)
				}
				if got.Nickname != tt.wantNickname {
					t.Fatalf("nickname = %q, want %q", got.Nickname, tt.wantNickname)
				}
			} else if err == nil {
				t.Fatalf("expected error, got result: %+v", got)
			}
		})
	}
}
