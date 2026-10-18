package disposal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

type fakeState struct {
	mu         sync.Mutex
	sessions   map[string]state.Session
	workspaces map[string]state.Workspace
}

func (f *fakeState) GetSessions() []state.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []state.Session
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out
}

func (f *fakeState) GetSession(id string) (state.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	return s, ok
}

func (f *fakeState) GetWorkspace(id string) (state.Workspace, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.workspaces[id]
	return w, ok
}

func (f *fakeState) remove(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, sessionID)
}

type fakeSessions struct {
	st       *fakeState
	mu       sync.Mutex
	marked   []string
	disposed []string
	failures map[string]error // dispose error by session ID
	// vanish makes Dispose fail while also removing the session, as a racing dispose would.
	vanish map[string]bool
}

func (f *fakeSessions) MarkSessionDisposing(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = append(f.marked, id)
	return "running", nil
}

func (f *fakeSessions) Dispose(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vanish[id] {
		f.st.remove(id)
		return fmt.Errorf("session not found: %s", id)
	}
	if err := f.failures[id]; err != nil {
		return err
	}
	f.disposed = append(f.disposed, id)
	f.st.remove(id)
	return nil
}

type fakeWorkspaces struct {
	st            *fakeState
	calls         []string
	prevStatus    string
	markErr       error
	remoteErr     error
	disposeErr    error
	cleanupErr    error
	cleanupCalls  []string
	reverted      []string
	disposedCtxOK bool
}

func (f *fakeWorkspaces) MarkWorkspaceDisposing(id string) (string, error) {
	f.calls = append(f.calls, "mark")
	return f.prevStatus, f.markErr
}
func (f *fakeWorkspaces) RevertWorkspaceStatus(id, prev string) {
	f.calls = append(f.calls, "revert")
	f.reverted = append(f.reverted, prev)
}
func (f *fakeWorkspaces) DeleteRemoteBranch(ctx context.Context, id string) error {
	f.calls = append(f.calls, "delete-remote-branch")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return f.remoteErr
}
func (f *fakeWorkspaces) DisposeForce(ctx context.Context, id string) error {
	f.calls = append(f.calls, "dispose-force")
	f.disposedCtxOK = ctx.Err() == nil
	return f.dispose(id)
}
func (f *fakeWorkspaces) DisposeForcePurge(ctx context.Context, id string) error {
	f.calls = append(f.calls, "dispose-force-purge")
	f.disposedCtxOK = ctx.Err() == nil
	return f.dispose(id)
}

func (f *fakeWorkspaces) CleanupFlightBranch(_ context.Context, repoURL, branch string) error {
	f.calls = append(f.calls, "cleanup-flight-branch")
	f.cleanupCalls = append(f.cleanupCalls, repoURL+" "+branch)
	return f.cleanupErr
}
func (f *fakeWorkspaces) dispose(id string) error {
	if f.disposeErr != nil {
		return f.disposeErr
	}
	f.st.mu.Lock()
	delete(f.st.workspaces, id)
	f.st.mu.Unlock()
	return nil
}

type fakePreviews struct {
	deleted []string
	err     error
}

func (f *fakePreviews) DeleteWorkspace(id string) error {
	f.deleted = append(f.deleted, id)
	return f.err
}

type env struct {
	svc        *Service
	st         *fakeState
	sessions   *fakeSessions
	workspaces *fakeWorkspaces
	previews   *fakePreviews
	broadcasts chan struct{}
	hooked     []string
	outcomes   []ResourceOutcome
}

func newEnv(t *testing.T, sessionIDs ...string) *env {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	st := &fakeState{sessions: map[string]state.Session{}, workspaces: map[string]state.Workspace{"ws-1": {ID: "ws-1"}}}
	for _, id := range sessionIDs {
		st.sessions[id] = state.Session{ID: id, WorkspaceID: "ws-1"}
	}
	st.sessions["other-session"] = state.Session{ID: "other-session", WorkspaceID: "ws-2"}
	e := &env{
		st:         st,
		sessions:   &fakeSessions{st: st, failures: map[string]error{}, vanish: map[string]bool{}},
		workspaces: &fakeWorkspaces{st: st, prevStatus: "running"},
		previews:   &fakePreviews{},
		broadcasts: make(chan struct{}, 16),
	}
	e.svc = &Service{
		Sessions:        e.sessions,
		Workspaces:      e.workspaces,
		Previews:        e.previews,
		State:           st,
		Broadcast:       func() { e.broadcasts <- struct{}{} },
		SessionDisposed: func(id string) { e.hooked = append(e.hooked, id) },
		Logger:          log.New(io.Discard),
	}
	return e
}

func (e *env) disposeAll(t *testing.T, req Request) (Result, error) {
	t.Helper()
	e.outcomes = nil
	return e.svc.DisposeAll(context.Background(), req, func(o ResourceOutcome) { e.outcomes = append(e.outcomes, o) })
}

func kinds(outcomes []ResourceOutcome) []string {
	out := make([]string, len(outcomes))
	for i, o := range outcomes {
		out[i] = o.Kind
		if o.Err != nil {
			out[i] += "!"
		}
	}
	return out
}

func writeChat(t *testing.T, workspaceID, sessionID string) string {
	t.Helper()
	p := filepath.Join(schmuxdir.ChatSessionDir(workspaceID, sessionID), "conversation.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDisposeAll_DisposesSessionsThenWorkspaceAndCleansPreviews(t *testing.T) {
	e := newEnv(t, "s1", "s2")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	got := append([]string(nil), res.SessionsDisposed...)
	sort.Strings(got)
	if strings.Join(got, ",") != "s1,s2" {
		t.Errorf("SessionsDisposed = %v", res.SessionsDisposed)
	}
	if !res.WorkspacePurged || res.WorkspaceMissing || res.AlreadyDisposing {
		t.Errorf("result = %+v", res)
	}
	if strings.Join(e.workspaces.calls, ",") != "mark,dispose-force" {
		t.Errorf("workspace calls = %v, want mark then dispose-force (no purge)", e.workspaces.calls)
	}
	if _, ok := e.st.GetSession("other-session"); !ok {
		t.Error("another workspace's session was disposed")
	}
	if strings.Join(kinds(e.outcomes), ",") != "session,session,workspace,preview" {
		t.Errorf("outcomes = %v (a plain dispose-all never touches chat records)", kinds(e.outcomes))
	}
	if strings.Join(e.previews.deleted, ",") != "ws-1" {
		t.Errorf("previews deleted for %v", e.previews.deleted)
	}
	if len(e.sessions.marked) != 2 {
		t.Errorf("sessions marked disposing = %v", e.sessions.marked)
	}
	sort.Strings(e.hooked)
	if strings.Join(e.hooked, ",") != "s1,s2" {
		t.Errorf("SessionDisposed hook saw %v", e.hooked)
	}
	// The final broadcast runs asynchronously. Five seconds bounds callback
	// scheduling; only receiving both events satisfies the broadcast claim.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for received := 0; received < 2; received++ {
		select {
		case <-e.broadcasts:
		case <-deadline.C:
			t.Fatalf("received %d/2 disposal broadcasts within 5s", received)
		}
	}
}

func TestDisposeAll_RecordsStepsAsTheyCompleteBeforeReturning(t *testing.T) {
	e := newEnv(t, "s1")
	var seenWhenSessionReported []string
	_, err := e.svc.DisposeAll(context.Background(), Request{WorkspaceID: "ws-1"}, func(o ResourceOutcome) {
		if o.Kind == "session" {
			// The workspace must not have been touched yet when a session outcome is reported.
			seenWhenSessionReported = append([]string(nil), e.workspaces.calls...)
		}
	})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	if strings.Join(seenWhenSessionReported, ",") != "mark" {
		t.Errorf("workspace calls when the session outcome was observed = %v", seenWhenSessionReported)
	}
}

func TestDisposeAll_SessionFailureWithoutForcePurgeStillDisposesWorkspace(t *testing.T) {
	e := newEnv(t, "s1", "s2")
	e.sessions.failures["s2"] = errors.New("tmux stuck")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v (dashboard dispose-all tolerates session failures)", err)
	}
	if strings.Join(res.SessionsDisposed, ",") != "s1" {
		t.Errorf("SessionsDisposed = %v, want only s1", res.SessionsDisposed)
	}
	if !res.WorkspacePurged {
		t.Error("workspace was not disposed")
	}
	failed := 0
	for _, o := range e.outcomes {
		if o.Kind == "session" && o.Err != nil && o.ID == "s2" {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("failed session outcomes = %d in %v", failed, kinds(e.outcomes))
	}
}

func TestDisposeAll_SessionFailureWithForcePurgeStopsBeforeDestroyingAnything(t *testing.T) {
	e := newEnv(t, "s1", "s2")
	e.sessions.failures["s2"] = errors.New("tmux stuck")
	chatFile := writeChat(t, "ws-1", "s1")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1", ForcePurge: true, ChatSessionIDs: []string{"s1", "s2"}})

	if err == nil || !strings.Contains(err.Error(), "tmux stuck") {
		t.Fatalf("DisposeAll() error = %v, want the session failure", err)
	}
	if res.WorkspacePurged || res.ChatDirectoriesDeleted != 0 {
		t.Errorf("result = %+v, want nothing destroyed", res)
	}
	if strings.Join(e.workspaces.calls, ",") != "mark,revert" || strings.Join(e.workspaces.reverted, ",") != "running" {
		t.Errorf("workspace calls = %v reverted to %v", e.workspaces.calls, e.workspaces.reverted)
	}
	if _, statErr := os.Stat(chatFile); statErr != nil {
		t.Errorf("chat history deleted despite a live session: %v", statErr)
	}
	if _, ok := e.st.GetWorkspace("ws-1"); !ok {
		t.Error("workspace removed")
	}
}

func TestDisposeAll_ForcePurgeDeletesChatHistoryEvenWithNoSessions(t *testing.T) {
	e := newEnv(t) // no live sessions: both were disposed in an earlier attempt
	gone1 := writeChat(t, "ws-1", "gone-1")
	writeChat(t, "ws-1", "gone-2")
	other := writeChat(t, "ws-2", "keep")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1", ForcePurge: true, ChatSessionIDs: []string{"gone-1", "gone-2"}})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	if res.ChatDirectoriesDeleted != 2 || !res.WorkspacePurged {
		t.Errorf("result = %+v", res)
	}
	if strings.Join(e.workspaces.calls, ",") != "mark,dispose-force-purge" {
		t.Errorf("workspace calls = %v, want force purge", e.workspaces.calls)
	}
	if _, statErr := os.Stat(gone1); !os.IsNotExist(statErr) {
		t.Errorf("gone-1 chat directory still present: %v", statErr)
	}
	if _, statErr := os.Stat(schmuxdir.ChatSessionDir("ws-1", "gone-2")); !os.IsNotExist(statErr) {
		t.Errorf("gone-2 chat directory still present: %v", statErr)
	}
	if _, statErr := os.Stat(other); statErr != nil {
		t.Errorf("another workspace's chat history was touched: %v", statErr)
	}
	if strings.Join(kinds(e.outcomes), ",") != "chat_storage,workspace,preview" {
		t.Errorf("outcomes = %v", kinds(e.outcomes))
	}
}

func TestDisposeAll_ChatHistoryIsKeptWhenNothingTargetsIt(t *testing.T) {
	e := newEnv(t) // no live sessions for ws-1
	chatFile := writeChat(t, "ws-1", "alone")

	if _, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"}); err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}

	if _, statErr := os.Stat(chatFile); statErr != nil {
		t.Errorf("chat history deleted without any session targeting it: %v", statErr)
	}
}

func TestDisposeAll_AlreadyGoneSessionCountsAsDisposed(t *testing.T) {
	e := newEnv(t, "s1")
	e.sessions.vanish["s1"] = true

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1", ForcePurge: true})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v (a session that vanished mid-dispose is already cleaned)", err)
	}
	if strings.Join(res.SessionsDisposed, ",") != "s1" || !res.WorkspacePurged {
		t.Errorf("result = %+v", res)
	}
}

func TestDisposeAll_MissingWorkspaceIsReportedNotFailed(t *testing.T) {
	e := newEnv(t)
	e.workspaces.markErr = errors.New("workspace not found: ws-9")

	res, err := e.svc.DisposeAll(context.Background(), Request{WorkspaceID: "ws-9", ForcePurge: true}, func(o ResourceOutcome) { e.outcomes = append(e.outcomes, o) })

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	if !res.WorkspaceMissing || res.WorkspacePurged {
		t.Errorf("result = %+v", res)
	}
	for _, c := range e.workspaces.calls {
		if strings.HasPrefix(c, "dispose") {
			t.Errorf("disposed a workspace that does not exist: %v", e.workspaces.calls)
		}
	}
	if strings.Join(kinds(e.outcomes), ",") != "workspace,preview" {
		t.Errorf("outcomes = %v", kinds(e.outcomes))
	}
}

func TestDisposeAll_MissingWorkspaceStillCleansRetainedFlightBranch(t *testing.T) {
	e := newEnv(t)
	e.workspaces.markErr = errors.New("workspace not found: ws-9")

	res, err := e.disposeAll(t, Request{
		WorkspaceID: "ws-9",
		ForcePurge:  true,
		BranchRepo:  "https://example.test/repo.git",
		BranchName:  "schmux/flight/fl-1234",
	})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	if !res.WorkspaceMissing {
		t.Fatalf("result = %+v", res)
	}
	if strings.Join(e.workspaces.cleanupCalls, ",") != "https://example.test/repo.git schmux/flight/fl-1234" {
		t.Fatalf("cleanup calls = %v", e.workspaces.cleanupCalls)
	}
	if strings.Join(kinds(e.outcomes), ",") != "workspace,branch,preview" {
		t.Errorf("outcomes = %v", kinds(e.outcomes))
	}
}

func TestDisposeAll_FlightBranchCleanupFailureIsRetryable(t *testing.T) {
	e := newEnv(t)
	e.workspaces.cleanupErr = errors.New("branch locked")

	_, err := e.disposeAll(t, Request{
		WorkspaceID: "ws-1",
		ForcePurge:  true,
		BranchRepo:  "https://example.test/repo.git",
		BranchName:  "schmux/flight/fl-1234",
	})

	if err == nil || !strings.Contains(err.Error(), "branch locked") {
		t.Fatalf("DisposeAll() error = %v, want branch cleanup failure", err)
	}
	if strings.Join(kinds(e.outcomes), ",") != "workspace,branch!" {
		t.Fatalf("outcomes = %v", kinds(e.outcomes))
	}
	if len(e.previews.deleted) != 0 {
		t.Fatalf("previews deleted after failed branch cleanup: %v", e.previews.deleted)
	}
}

func TestDisposeAll_AlreadyDisposingWorkspace(t *testing.T) {
	t.Run("plain dispose-all returns without touching anything", func(t *testing.T) {
		e := newEnv(t, "s1")
		e.workspaces.prevStatus = state.WorkspaceStatusDisposing

		res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"})

		if err != nil || !res.AlreadyDisposing {
			t.Fatalf("result = %+v, error = %v", res, err)
		}
		if len(e.sessions.marked) != 0 || strings.Join(e.workspaces.calls, ",") != "mark" {
			t.Errorf("work started on an already-disposing workspace: sessions %v, workspace %v", e.sessions.marked, e.workspaces.calls)
		}
	})
	t.Run("force purge resumes an interrupted disposal", func(t *testing.T) {
		e := newEnv(t, "s1")
		e.workspaces.prevStatus = state.WorkspaceStatusDisposing

		res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1", ForcePurge: true})

		if err != nil || res.AlreadyDisposing || !res.WorkspacePurged {
			t.Fatalf("result = %+v, error = %v", res, err)
		}
	})
}

func TestDisposeAll_WorkspaceFailureRevertsStatusAndReturnsTheError(t *testing.T) {
	e := newEnv(t, "s1")
	e.workspaces.disposeErr = errors.New("device busy")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"})

	if err == nil || err.Error() != "device busy" {
		t.Fatalf("DisposeAll() error = %v, want the workspace error unchanged", err)
	}
	if res.WorkspacePurged {
		t.Error("result claims the workspace was purged")
	}
	if strings.Join(e.workspaces.calls, ",") != "mark,dispose-force,revert" {
		t.Errorf("workspace calls = %v", e.workspaces.calls)
	}
	last := e.outcomes[len(e.outcomes)-1]
	if last.Kind != "workspace" || last.Err == nil {
		t.Errorf("last outcome = %+v, want a failed workspace outcome", last)
	}
}

func TestDisposeAll_PreviewCleanupFailureIsReportedButNotFatal(t *testing.T) {
	e := newEnv(t)
	e.previews.err = errors.New("proxy busy")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"})

	if err != nil || !res.WorkspacePurged {
		t.Fatalf("result = %+v, error = %v", res, err)
	}
	last := e.outcomes[len(e.outcomes)-1]
	if last.Kind != "preview" || last.Err == nil {
		t.Errorf("last outcome = %+v", last)
	}
}

func TestDisposeAll_NilPreviewsAndObserverAreAllowed(t *testing.T) {
	e := newEnv(t, "s1")
	e.svc.Previews = nil

	if _, err := e.svc.DisposeAll(context.Background(), Request{WorkspaceID: "ws-1"}, nil); err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
}

func TestDisposeAll_RemoteBranchDeletionRunsFirstAndAbortsOnFailure(t *testing.T) {
	notMerged := errors.New("not merged")
	e := newEnv(t, "s1")
	e.workspaces.remoteErr = fmt.Errorf("wrapped: %w", notMerged)

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1", DeleteRemoteBranch: true})

	if !errors.Is(err, notMerged) {
		t.Fatalf("DisposeAll() error = %v, want it to wrap the remote branch error", err)
	}
	if strings.Join(e.workspaces.calls, ",") != "delete-remote-branch" || len(e.sessions.marked) != 0 || len(res.SessionsDisposed) != 0 {
		t.Errorf("work happened after a failed remote branch delete: workspace %v, sessions %v", e.workspaces.calls, e.sessions.marked)
	}
	if strings.Join(kinds(e.outcomes), ",") != "branch!" {
		t.Errorf("outcomes = %v", kinds(e.outcomes))
	}
}

func TestDisposeAll_CallerCancellationDoesNotInterruptDisposal(t *testing.T) {
	e := newEnv(t, "s1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the HTTP client went away

	res, err := e.svc.DisposeAll(ctx, Request{WorkspaceID: "ws-1"}, nil)

	if err != nil || !res.WorkspacePurged {
		t.Fatalf("result = %+v, error = %v", res, err)
	}
	if !e.workspaces.disposedCtxOK {
		t.Error("workspace disposal ran on the caller's cancelled context")
	}
}

func TestDisposeAll_PlainRequestKeepsLiveSessionConversations(t *testing.T) {
	e := newEnv(t, "s1")
	chatFile := writeChat(t, "ws-1", "s1")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1"})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	if strings.Join(res.SessionsDisposed, ",") != "s1" || res.ChatDirectoriesDeleted != 0 {
		t.Errorf("result = %+v, want s1 disposed and no chat deleted", res)
	}
	if _, statErr := os.Stat(chatFile); statErr != nil {
		t.Errorf("ordinary dispose-all deleted a live session's conversation: %v", statErr)
	}
	if strings.Join(kinds(e.outcomes), ",") != "session,workspace,preview" {
		t.Errorf("outcomes = %v", kinds(e.outcomes))
	}
}

func TestDisposeAll_ChatSessionIDsAlsoCoverLiveSessions(t *testing.T) {
	e := newEnv(t, "s1")
	live := writeChat(t, "ws-1", "s1")
	prior := writeChat(t, "ws-1", "prior")
	other := writeChat(t, "ws-1", "unrelated")

	res, err := e.disposeAll(t, Request{WorkspaceID: "ws-1", ForcePurge: true, ChatSessionIDs: []string{"prior"}})

	if err != nil {
		t.Fatalf("DisposeAll() error = %v", err)
	}
	if res.ChatDirectoriesDeleted != 2 {
		t.Errorf("ChatDirectoriesDeleted = %d, want 2", res.ChatDirectoriesDeleted)
	}
	for _, p := range []string{live, prior} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s still present: %v", p, statErr)
		}
	}
	if _, statErr := os.Stat(other); statErr != nil {
		t.Errorf("unrelated conversation under the same workspace ID was touched: %v", statErr)
	}
}
