//go:build !nogithub

package dashboard

import (
	"context"
	"testing"

	"github.com/sergeknystautas/schmux/internal/github"
)

func TestPRTracker_DropExceptRemovesUnknowns(t *testing.T) {
	tr := NewPRTracker()
	tr.entries["ws-keep"] = PRRef{Number: 1, URL: "https://pr/1"}
	tr.entries["ws-drop"] = PRRef{Number: 2, URL: "https://pr/2"}

	if !tr.DropExcept(map[string]bool{"ws-keep": true}) {
		t.Error("DropExcept = false, want true when entries were dropped")
	}
	if _, ok := tr.Get("ws-keep"); !ok {
		t.Error("ws-keep should survive DropExcept")
	}
	if _, ok := tr.Get("ws-drop"); ok {
		t.Error("ws-drop should be evicted")
	}
}

func TestPRTracker_ClearResets(t *testing.T) {
	tr := NewPRTracker()
	tr.entries["ws1"] = PRRef{Number: 1, URL: "https://pr/1"}

	if !tr.Clear() {
		t.Error("Clear = false, want true when entries were cleared")
	}
	if _, ok := tr.Get("ws1"); ok {
		t.Error("ws1 should be cleared")
	}
	if tr.Clear() {
		t.Error("Clear = true on empty tracker, want false")
	}
}

func TestPRTracker_DropExceptEmptyNoOp(t *testing.T) {
	tr := NewPRTracker()
	if tr.DropExcept(map[string]bool{}) {
		t.Error("DropExcept = true on empty tracker, want false")
	}
}

func TestPRTracker_GetReturnsFalseWhenAbsent(t *testing.T) {
	tr := NewPRTracker()
	if _, ok := tr.Get("missing"); ok {
		t.Error("Get on empty tracker returned ok=true")
	}
}

func TestPRTracker_RefreshNoWorkspacesIsNoOp(t *testing.T) {
	tr := NewPRTracker()
	if tr.Refresh(context.TODO(), "", github.RepoInfo{Owner: "acme", Repo: "app"}, nil) {
		t.Error("Refresh with empty workspaces should not report changed")
	}
	if _, ok := tr.Get("ws1"); ok {
		t.Error("Refresh with empty workspaces should not populate")
	}
}
