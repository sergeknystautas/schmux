# Go Architecture Improvement Tasks

Based on architectural review dated 2025-01-08.

**Focus:** Things that actually slow down developers.

---

## How to Use This Doc

### For Agents: Claiming and Completing Tasks

1. **Before starting**: Read the task details below
2. **Generate a plan** based on the task description
3. **Do the work**
4. **When done**: Update the Status column (⬜ → ✅) and commit to git
5. **No ⬜ → 🔄**: We don't track in-progress, only done

### Example Command to an Agent

```
"Read GOLANG-TASKS.md, do Task #2, and give me a plan first"
```

The agent should:
1. Read this file
2. Find Task #2 (Add Interfaces for Testability)
3. Read the implementation details
4. Present a specific plan for approval
5. Execute once approved

---

## Legend

### Priority (How Important)
- **P1** - Critical: Blocks or significantly slows development
- **P2** - Medium: Helps but not blocking

### Parallelizability (Can Multiple Agents Work Simultaneously?)
- 🔴 **Full Block** - Only ONE agent (would create conflicts)
- 🟡 **Partial Block** - Some parts can be parallel, some can't
- 🟢 **Parallel Safe** - Multiple agents OK (different files/tasks)

### Task Status
- ⬜ **Todo** - Not started
- 🔄 **In Progress** - Currently being worked on
- ✅ **Done** - Completed
- 🚫 **Blocked** - Dependency failed or blocked

---

## P1 CRITICAL TASKS

### 1. Fix Resource Leaks [🟢 Parallel Safe] - ⬜

**Why P1:** File descriptor leaks cause mysterious bugs that waste hours debugging

**Implementation:**

1. **Fix known leak at `internal/session/manager.go:275`**:
   ```go
   // Current (BAD):
   fd, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
   if err != nil {
       return "", fmt.Errorf("failed to create log file: %w", err)
   }
   fd.Close()  // Not deferred - error path leak!

   // Fix:
   fd, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
   if err != nil {
       return "", fmt.Errorf("failed to create log file: %w", err)
   }
   defer fd.Close()
   ```

2. **Audit each package** for:
   - `os.OpenFile(...)` - check that `defer f.Close()` follows
   - `os.Create(...)` - check that `defer f.Close()` follows
   - `os.Open(...)` - check that `defer f.Close()` follows

3. **Fix any found leaks** by adding `defer` immediately after the error check

**Files to audit:**
- `internal/config/*.go`
- `internal/state/*.go`
- `internal/session/*.go` (including known leak at :275)
- `internal/workspace/*.go`
- `internal/dashboard/*.go`
- `internal/daemon/*.go`
- `internal/tmux/*.go`

**Feature work OK:** Different packages, different files

---

### 2. Add Interfaces for Testability [🔴 Full Block] - ✅

**Why P1:** Can't mock anything, tests are slow/fragile, have to run real tmux/git

**Implementation:**

1. **Create `internal/state/interfaces.go`**:
   ```go
   package state

   type Session struct {
       ID            string
       WorkspaceID   string
       Agent         string
       Prompt        string
       Nickname      string
       CreatedAt     time.Time
       LastOutputAt  time.Time
   }

   type Workspace struct {
       ID     string
       Repo   string
       Branch string
       Path   string
   }

   // StateStore defines the interface for state persistence
   type StateStore interface {
       GetSessions() []Session
       GetSession(id string) (Session, bool)
       AddSession(sess Session) error
       UpdateSession(sess Session) error
       RemoveSession(id string) error

       GetWorkspaces() []Workspace
       GetWorkspace(id string) (Workspace, bool)
       AddWorkspace(ws Workspace) error
       UpdateWorkspace(ws Workspace) error
       RemoveWorkspace(id string) error

       Save() error
   }
   ```

2. **Create `internal/tmux/interfaces.go`**:
   ```go
   package tmux

   // TmuxService defines the interface for tmux operations
   type TmuxService interface {
       CreateSession(name, dir, command string) error
       KillSession(name string) error
       ListSessions() ([]string, error)
       SessionExists(name string) bool
       SendKeys(session, window, pane string, keys string) error
       CapturePane(session, window string) (string, error)
       SetWindowSize(session string, width, height int) error
   }
   ```

3. **Create `internal/workspace/interfaces.go`**:
   ```go
   package workspace

   // WorkspaceManager defines the interface for workspace operations
   type WorkspaceManager interface {
       Clone(repoURL, branch, workspaceDir string) (string, error)
       Checkout(workspaceDir, branch string) error
       Scan() (added, removed int, err error)
       Dispose(workspaceID string) error
   }
   ```

4. **Refactor structs to accept interfaces**:
   ```go
   // Before:
   type Manager struct {
       config    *config.Config
       state     *state.State
       workspace *workspace.Manager
       tmux      *tmux.Tmux
   }

   // After:
   type Manager struct {
       config    *config.Config
       state     state.StateStore
       workspace workspace.WorkspaceManager
       tmux      tmux.TmuxService
   }

   func NewManager(cfg *config.Config, st state.StateStore, wm workspace.WorkspaceManager, tm tmux.TmuxService) *Manager {
       // ...
   }
   ```

**Files to modify:**
- Create: `internal/state/interfaces.go`
- Create: `internal/tmux/interfaces.go`
- Create: `internal/workspace/interfaces.go`
- Modify: `internal/session/manager.go`
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/dashboard/server.go`

**Blocks feature work:** Changes core interfaces that everything depends on

---

### 3. Add Timeouts to External Operations [🔴 Full Block] - ✅

**Why P1:** External calls (tmux, git) hang forever when things go wrong

**Implementation:**

1. **Add `ctx` parameter to all tmux functions**:
   ```go
   // Before:
   func CreateSession(name, dir, command string) error {
       cmd := exec.Command("tmux", args...)
       // ...
   }

   // After:
   func CreateSession(ctx context.Context, name, dir, command string) error {
       cmd := exec.CommandContext(ctx, "tmux", args...)
       // ...
   }
   ```

2. **Add `ctx` to workspace.Manager methods**:
   ```go
   func (m *Manager) Clone(ctx context.Context, repoURL, branch, workspaceDir string) (string, error) {
       cmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "clone", "--branch", branch, repoURL, workspaceDir)
       // ...
   }
   ```

3. **Add `ctx` to session.Manager**:
   ```go
   func (m *Manager) Spawn(ctx context.Context, repo, branch, agent, prompt, nickname, workspaceID string) (*Session, error) {
       // Pass ctx through to workspace and tmux calls
   }
   ```

4. **Use `context.WithTimeout` at call sites**:
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   err := tmux.CreateSession(ctx, name, dir, command)
   if err == context.DeadlineExceeded {
       return fmt.Errorf("tmux timed out after 30s")
   }
   ```

**Files to modify:**
- `internal/tmux/tmux.go` - all functions
- `internal/workspace/manager.go` - Clone, Checkout, Scan
- `internal/session/manager.go` - Spawn, Attach, Status
- `internal/daemon/daemon.go` - pass through to managers

**Blocks feature work:** Changes function signatures that everything depends on

---

### 4. Fix Silent Error Handling [🔴 Full Block] - ⬜

**Why P1:** Errors printed to console instead of returned = impossible to debug

**Implementation:**

1. **Find all silent error prints**:
   ```bash
   grep -r 'fmt.Printf("warning' internal/
   grep -r 'fmt.Printf("Warning' internal/
   grep -r 'fmt.Println("warning' internal/
   ```

2. **Example fix**:
   ```go
   // Before (BAD):
   if err := tmux.SetWindowSizeManual(tmuxSession); err != nil {
       fmt.Printf("warning: failed to set manual window size: %v\n", err)
   }

   // After:
   if err := tmux.SetWindowSizeManual(tmuxSession); err != nil {
       return fmt.Errorf("failed to set manual window size: %w", err)
   }
   ```

3. **Pattern: change signature to return error**:
   ```go
   // Before:
   func (m *Manager) DoThing(id string) {
       if err := something(); err != nil {
           fmt.Printf("warning: %v\n", err)
       }
   }

   // After:
   func (m *Manager) DoThing(id string) error {
       if err := something(); err != nil {
           return fmt.Errorf("something failed: %w", err)
       }
       return nil
   }
   ```

**Files to modify:**
- `internal/session/*.go`
- `internal/workspace/*.go`
- `internal/daemon/*.go`
- `internal/dashboard/*.go`

**Blocks feature work:** Changes function signatures that other code depends on

---

### 5. Remove Global State [🟢 Parallel Safe] - ⬜

**Why P1:** Package-level `shutdownChan` makes testing impossible

**Must complete after:** Add Timeouts (Task 3)

**Implementation:**

1. **Move package-level `shutdownChan` into Daemon struct**:
   ```go
   // Before (in internal/daemon/daemon.go):
   var shutdownChan = make(chan struct{})

   type Daemon struct {
       // ...
   }

   // After:
   type Daemon struct {
       shutdownChan chan struct{}
       // ...
   }

   func NewDaemon(...) *Daemon {
       return &Daemon{
           shutdownChan: make(chan struct{}),
           // ...
       }
   }
   ```

2. **Add `Shutdown()` method**:
   ```go
   func (d *Daemon) Shutdown() {
       close(d.shutdownChan)
   }
   ```

3. **Update signal handlers in `cmd/schmux/main.go`**:
   ```go
   // Before:
   sigChan := make(chan os.Signal, 1)
   signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
   <-sigChan
   close(shutdownChan)

   // After:
   sigChan := make(chan os.Signal, 1)
   signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
   <-sigChan
   daemon.Shutdown()
   ```

4. **Add `Wait()` for goroutine cleanup**:
   ```go
   type Daemon struct {
       shutdownChan chan struct{}
       wg           sync.WaitGroup
       // ...
   }

   func (d *Daemon) Wait() {
       d.wg.Wait()
   }
   ```

**Files to modify:**
- `internal/daemon/daemon.go`
- `cmd/schmux/main.go`

**Feature work OK:** Only touches daemon.go and main.go, feature work doesn't use these

---

## P2 MEDIUM PRIORITY TASKS

### 6. Basic Handler Tests [🟡 Partial Block] - ⬜

**Why P2:** Can't change dashboard code without fear of breaking things

**Must complete after:** Add Interfaces (Task 2)

**Implementation:**

1. **Create mock implementations**:
   ```go
   // In internal/dashboard/mocks_test.go:
   type MockStateStore struct {
       // implement StateStore interface
   }

   type MockTmuxService struct {
       // implement TmuxService interface
   }
   ```

2. **Test example**:
   ```go
   func TestHandleSessions(t *testing.T) {
       mockState := &MockStateStore{...}
       mockSession := &MockSessionManager{...}

       server := NewServer(mockState, mockSession, ...)
       req := httptest.NewRequest("GET", "/api/sessions", nil)
       w := httptest.NewRecorder()

       server.handleSessions(w, req)

       if w.Code != http.StatusOK {
           t.Errorf("expected 200, got %d", w.Code)
       }
   }
   ```

3. **Test for WebSocket handler** - use `gorilla/websocket` for test clients

**Files to create/modify:**
- Create: `internal/dashboard/mocks_test.go`
- Create: `internal/dashboard/handlers_test.go`
- Create: `internal/dashboard/websocket_test.go`

**Feature work OK:** Same file but different test functions - git can usually merge

---

### 7. Concurrency Tests [🟡 Partial Block] - ⬜

**Why P2:** Race conditions are subtle and waste time to debug

**Implementation:**

1. **Add `go test -race` to CI** (GitHub Actions or similar):
   ```yaml
   # In .github/workflows/test.yml
   - name: Test
     run: go test -race ./...
   ```

2. **Stress test for state package**:
   ```go
   // In internal/state/stress_test.go:
   func TestConcurrentSessionMutations(t *testing.T) {
       st := New()
       var wg sync.WaitGroup

       for i := 0; i < 100; i++ {
           wg.Add(1)
           go func(id string) {
               defer wg.Done()
               st.AddSession(Session{ID: id})
           }(strconv.Itoa(i))
       }

       wg.Wait()
       sessions := st.GetSessions()
       if len(sessions) != 100 {
           t.Errorf("expected 100 sessions, got %d", len(sessions))
       }
   }
   ```

3. **Concurrent tests for session package** - test race conditions in session lifecycle

**Files to create/modify:**
- `.github/workflows/*.yml` or equivalent CI config
- Create: `internal/state/stress_test.go`
- Create: `internal/session/concurrent_test.go`

**Feature work OK:** CI config is one task; test files can be done by different agents

---

## Task Dependencies

```
Add Interfaces (2) [P1, 🔴]
    ├─→ Add Timeouts (3) [P1, 🔴]
    │       └─→ Remove Global State (5) [P1, 🔴]
    │
    └─→ Handler Tests (6) [P2, 🟡]

Fix Resource Leaks (1) [P1, 🟢] [Independent]
Fix Silent Errors (4) [P1, 🔴] [Independent]
Remove Global State (5) [P1, 🟢] [Depends: 3]
Concurrency Tests (7) [P2, 🟡] [Independent]
```

---

## Parallel Execution Strategy

### Can Start Immediately (No Dependencies)

| Task | Priority | Parallelizability |
|------|----------|-------------------|
| Fix Resource Leaks (1) | P1 | 🟢 Feature work OK |
| Remove Global State (5) | P1 | 🟢 Feature work OK |
| Concurrency Tests (7) | P2 | 🟡 Feature work OK |

### Must Do In Order

| Order | Task | Priority | Parallelizability |
|-------|------|----------|-------------------|
| 1st | Add Interfaces (2) | P1 | 🔴 Blocks feature work |
| 2nd | Add Timeouts (3) | P1 | 🔴 Blocks feature work |
| 3rd | Fix Silent Errors (4) | P1 | 🔴 Blocks feature work |
| 4th | Handler Tests (6) | P2 | 🟡 Feature work OK |

---

## Progress Tracking

**Total Tasks:** 7
**Completed:** 2
**In Progress:** 0
**Blocked:** 0

### By Priority
- **P1 Critical:** 5 tasks (40% complete)
- **P2 Medium:** 2 tasks (0% complete)

### Completed Tasks
- ✅ **Task #2: Add Interfaces for Testability** - Created StateStore, TmuxService, WorkspaceManager interfaces; refactored structs to accept interfaces; added error returns to state methods; added error path tests
- ✅ **Task #3: Add Timeouts to External Operations** - Added context.Context parameter to all tmux, workspace, and session manager functions; used exec.CommandContext for all external commands; added appropriate timeouts at call sites (5s for tmux queries, 10s for tmux operations, 30s for git status, 5m for spawn operations)

### By Parallelizability
- 🔴 **Full Block:** 3 tasks (blocks feature work)
- 🟡 **Partial Block:** 2 tasks (feature work OK)
- 🟢 **Parallel Safe:** 2 tasks (feature work OK)

---

## Notes for Agents

1. **🔴 Full Block** = Stop all feature work while doing this (changes signatures everything depends on)
2. **🟡 Partial Block** = Some feature work OK, but be careful
3. **🟢 Parallel Safe** = Feature work can continue normally
4. **Check dependencies** - Tasks 3 and 5 need earlier tasks to complete
5. **Update status** (⬜ → 🔄 → ✅ → 🚫) as you work
6. **Test after completion** - run `go test ./...` for code changes
