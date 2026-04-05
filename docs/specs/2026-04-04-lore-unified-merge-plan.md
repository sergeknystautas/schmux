# Plan: Lore Unified Merge & Push

**Goal**: Replace the per-proposal merge/push flow with a unified per-repo PendingMerge model — one diff, server-persisted state, temporary worktrees instead of persistent workspaces.

**Architecture**: New `PendingMergeStore` in `internal/lore/` with mutex-protected JSON file storage. New repo-level merge/push/edit endpoints replace per-proposal merge/apply-merge. Frontend derives phase from server state via `GET /pending-merge`. Temporary git worktree for commit+push, cleaned up immediately.

**Tech Stack**: Go (backend), TypeScript/React (frontend), chi router, Vitest + React Testing Library (frontend tests)

**Design**: `docs/specs/2026-04-04-lore-unified-merge-design.md`

---

## Step 1: Create PendingMergeStore data model and CRUD

**Files**: Create `internal/lore/pending_merge.go`, Create `internal/lore/pending_merge_test.go`

### 1a. Write failing test

```go
// internal/lore/pending_merge_test.go
package lore

import (
    "testing"
    "time"
)

func TestPendingMergeStore_SaveAndGet(t *testing.T) {
    store := NewPendingMergeStore(t.TempDir(), nil)
    pm := &PendingMerge{
        Repo:           "myrepo",
        Status:         PendingMergeStatusMerging,
        BaseSHA:        "abc123",
        RuleIDs:        []string{"r1", "r2"},
        ProposalIDs:    []string{"prop-001"},
        CreatedAt:      time.Now().UTC(),
    }
    if err := store.Save(pm); err != nil {
        t.Fatalf("Save: %v", err)
    }
    got, err := store.Get("myrepo")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if got.Status != PendingMergeStatusMerging {
        t.Errorf("status = %q, want %q", got.Status, PendingMergeStatusMerging)
    }
    if len(got.RuleIDs) != 2 {
        t.Errorf("rule_ids len = %d, want 2", len(got.RuleIDs))
    }
}

func TestPendingMergeStore_GetNotFound(t *testing.T) {
    store := NewPendingMergeStore(t.TempDir(), nil)
    _, err := store.Get("nonexistent")
    if err == nil {
        t.Fatal("expected error for nonexistent repo")
    }
}

func TestPendingMergeStore_Delete(t *testing.T) {
    store := NewPendingMergeStore(t.TempDir(), nil)
    pm := &PendingMerge{Repo: "myrepo", Status: PendingMergeStatusReady}
    store.Save(pm)
    if err := store.Delete("myrepo"); err != nil {
        t.Fatalf("Delete: %v", err)
    }
    _, err := store.Get("myrepo")
    if err == nil {
        t.Fatal("expected error after delete")
    }
}

func TestPendingMergeStore_UpdateEdited(t *testing.T) {
    store := NewPendingMergeStore(t.TempDir(), nil)
    pm := &PendingMerge{Repo: "myrepo", Status: PendingMergeStatusReady, MergedContent: "original"}
    store.Save(pm)
    edited := "user-edited content"
    if err := store.UpdateEditedContent("myrepo", &edited); err != nil {
        t.Fatalf("UpdateEditedContent: %v", err)
    }
    got, _ := store.Get("myrepo")
    if got.EditedContent == nil || *got.EditedContent != edited {
        t.Errorf("edited_content = %v, want %q", got.EditedContent, edited)
    }
}

func TestPendingMergeStore_Expired(t *testing.T) {
    store := NewPendingMergeStore(t.TempDir(), nil)
    pm := &PendingMerge{
        Repo:      "myrepo",
        Status:    PendingMergeStatusReady,
        CreatedAt: time.Now().Add(-25 * time.Hour),
    }
    store.Save(pm)
    got, err := store.Get("myrepo")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if !got.IsExpired() {
        t.Error("expected IsExpired() = true for 25h-old merge")
    }
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/lore/ -run TestPendingMergeStore -count=1
```

### 1c. Write implementation

```go
// internal/lore/pending_merge.go
package lore

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/charmbracelet/log"
)

const (
    PendingMergeStatusMerging = "merging"
    PendingMergeStatusReady   = "ready"
    PendingMergeStatusError   = "error"

    pendingMergeTTL = 24 * time.Hour
)

type PendingMerge struct {
    Repo           string    `json:"repo"`
    Status         string    `json:"status"`
    BaseSHA        string    `json:"base_sha"`
    RuleIDs        []string  `json:"rule_ids"`
    ProposalIDs    []string  `json:"proposal_ids"`
    MergedContent  string    `json:"merged_content"`
    CurrentContent string    `json:"current_content"`
    Summary        string    `json:"summary"`
    EditedContent  *string   `json:"edited_content,omitempty"`
    Error          string    `json:"error,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
}

func (pm *PendingMerge) IsExpired() bool {
    return time.Since(pm.CreatedAt) > pendingMergeTTL
}

// EffectiveContent returns EditedContent if set, otherwise MergedContent.
func (pm *PendingMerge) EffectiveContent() string {
    if pm.EditedContent != nil {
        return *pm.EditedContent
    }
    return pm.MergedContent
}

type PendingMergeStore struct {
    baseDir string
    logger  *log.Logger
    mu      sync.Mutex
}

func NewPendingMergeStore(baseDir string, logger *log.Logger) *PendingMergeStore {
    return &PendingMergeStore{baseDir: baseDir, logger: logger}
}

func (s *PendingMergeStore) path(repo string) string {
    return filepath.Join(s.baseDir, repo, "pending-merge.json")
}

func (s *PendingMergeStore) Save(pm *PendingMerge) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.saveLocked(pm)
}

func (s *PendingMergeStore) saveLocked(pm *PendingMerge) error {
    if err := validateRepoName(pm.Repo); err != nil {
        return err
    }
    dir := filepath.Join(s.baseDir, pm.Repo)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return err
    }
    data, err := json.MarshalIndent(pm, "", "  ")
    if err != nil {
        return err
    }
    destPath := s.path(pm.Repo)
    tmp, err := os.CreateTemp(dir, ".pending-merge-*.tmp")
    if err != nil {
        return fmt.Errorf("create temp file: %w", err)
    }
    tmpPath := tmp.Name()
    if _, err := tmp.Write(data); err != nil {
        tmp.Close()
        os.Remove(tmpPath)
        return err
    }
    if err := tmp.Close(); err != nil {
        os.Remove(tmpPath)
        return err
    }
    return os.Rename(tmpPath, destPath)
}

func (s *PendingMergeStore) Get(repo string) (*PendingMerge, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.getLocked(repo)
}

func (s *PendingMergeStore) getLocked(repo string) (*PendingMerge, error) {
    if err := validateRepoName(repo); err != nil {
        return nil, err
    }
    data, err := os.ReadFile(s.path(repo))
    if err != nil {
        return nil, err
    }
    var pm PendingMerge
    if err := json.Unmarshal(data, &pm); err != nil {
        return nil, err
    }
    return &pm, nil
}

func (s *PendingMergeStore) Delete(repo string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if err := validateRepoName(repo); err != nil {
        return err
    }
    err := os.Remove(s.path(repo))
    if os.IsNotExist(err) {
        return nil
    }
    return err
}

// UpdateEditedContent is a read-modify-write for user edits.
func (s *PendingMergeStore) UpdateEditedContent(repo string, content *string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    pm, err := s.getLocked(repo)
    if err != nil {
        return err
    }
    pm.EditedContent = content
    return s.saveLocked(pm)
}

// InvalidateIfContainsRule deletes the PendingMerge if it references the given rule ID.
// Called when a rule is unapproved, edited, or dismissed after merge.
func (s *PendingMergeStore) InvalidateIfContainsRule(repo, ruleID string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    pm, err := s.getLocked(repo)
    if err != nil {
        return
    }
    for _, id := range pm.RuleIDs {
        if id == ruleID {
            os.Remove(s.path(repo))
            return
        }
    }
}
```

### 1d. Run test to verify it passes

```bash
go test ./internal/lore/ -run TestPendingMergeStore -count=1
```

### 1e. Commit

```bash
git commit -m "feat(lore): add PendingMergeStore data model with CRUD and TTL"
```

---

## Step 2: Wire PendingMergeStore into Server

**Files**: Modify `internal/dashboard/server.go`

### 2a. No test needed — wiring only

### 2b. Write implementation

Add to `Server` struct (around line 199, near `loreInstructionStore`):

```go
lorePendingMergeStore *lore.PendingMergeStore
```

Add setter method (after `SetLoreInstructionStore`):

```go
func (s *Server) SetLorePendingMergeStore(store *lore.PendingMergeStore) {
    s.lorePendingMergeStore = store
}
```

Initialize in daemon startup (wherever `SetLoreStore` is called — search for `SetLoreStore` in `internal/daemon/`):

```go
pendingMergeStore := lore.NewPendingMergeStore(loreDir, logger)
server.SetLorePendingMergeStore(pendingMergeStore)
```

### 2c. Verify build

```bash
go build ./cmd/schmux
```

### 2d. Commit

```bash
git commit -m "feat(lore): wire PendingMergeStore into Server and daemon startup"
```

---

## Step 3: Add GET /api/lore/{repo}/pending-merge endpoint

**Files**: Modify `internal/dashboard/handlers_lore.go`, Modify `internal/dashboard/server.go`

### 3a. Write failing test

Add to `internal/dashboard/handlers_lore_test.go`:

```go
func TestHandleLorePendingMerge_NotFound(t *testing.T) {
    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "config.json")
    cfg := config.CreateDefault(configPath)
    statePath := filepath.Join(tmpDir, "state.json")
    st := state.New(statePath, nil)
    logger := log.NewWithOptions(io.Discard, log.Options{})
    wm := workspace.New(cfg, st, statePath, logger)
    sm := session.New(cfg, st, statePath, wm, logger)
    shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
    defer shutdownCancel()
    server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
    server.SetModelManager(models.New(cfg, nil, "", logger))
    server.SetLorePendingMergeStore(lore.NewPendingMergeStore(t.TempDir(), nil))
    defer server.CloseForTest()

    req := httptest.NewRequest(http.MethodGet, "/api/lore/testrepo/pending-merge", nil)
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add("repo", "testrepo")
    req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
    rr := httptest.NewRecorder()
    server.handleLorePendingMergeGet(rr, req)

    if rr.Code != http.StatusNotFound {
        t.Errorf("expected 404, got %d", rr.Code)
    }
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/dashboard/ -run TestHandleLorePendingMerge_NotFound -count=1
```

### 3c. Write implementation

In `handlers_lore.go`, add handler:

```go
func (s *Server) handleLorePendingMergeGet(w http.ResponseWriter, r *http.Request) {
    repoName := chi.URLParam(r, "repo")
    if s.lorePendingMergeStore == nil {
        http.Error(w, "pending merge store not configured", http.StatusServiceUnavailable)
        return
    }
    pm, err := s.lorePendingMergeStore.Get(repoName)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(pm)
}
```

In `server.go`, register the route inside `r.Route("/lore/{repo}", ...)` (around line 756):

```go
r.Get("/pending-merge", s.handleLorePendingMergeGet)
```

### 3d. Run test to verify it passes

```bash
go test ./internal/dashboard/ -run TestHandleLorePendingMerge -count=1
```

### 3e. Commit

```bash
git commit -m "feat(lore): add GET /api/lore/{repo}/pending-merge endpoint"
```

---

## Step 4: Add DELETE and PATCH /api/lore/{repo}/pending-merge endpoints

**Files**: Modify `internal/dashboard/handlers_lore.go`, Modify `internal/dashboard/server.go`, Modify `internal/dashboard/handlers_lore_test.go`

### 4a. Write tests

Add to `internal/dashboard/handlers_lore_test.go`:

```go
func TestHandleLorePendingMergePatch(t *testing.T) {
    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "config.json")
    cfg := config.CreateDefault(configPath)
    statePath := filepath.Join(tmpDir, "state.json")
    st := state.New(statePath, nil)
    logger := log.NewWithOptions(io.Discard, log.Options{})
    wm := workspace.New(cfg, st, statePath, logger)
    sm := session.New(cfg, st, statePath, wm, logger)
    shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
    defer shutdownCancel()
    server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
    server.SetModelManager(models.New(cfg, nil, "", logger))
    defer server.CloseForTest()

    pmStore := lore.NewPendingMergeStore(t.TempDir(), nil)
    server.SetLorePendingMergeStore(pmStore)
    pmStore.Save(&lore.PendingMerge{Repo: "testrepo", Status: lore.PendingMergeStatusReady, MergedContent: "original"})

    body, _ := json.Marshal(map[string]string{"edited_content": "user edit"})
    req := httptest.NewRequest(http.MethodPatch, "/api/lore/testrepo/pending-merge", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add("repo", "testrepo")
    req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
    rr := httptest.NewRecorder()
    server.handleLorePendingMergePatch(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
    }
    pm, _ := pmStore.Get("testrepo")
    if pm.EditedContent == nil || *pm.EditedContent != "user edit" {
        t.Errorf("expected edited_content='user edit', got %v", pm.EditedContent)
    }
}

func TestHandleLorePendingMergeDelete(t *testing.T) {
    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "config.json")
    cfg := config.CreateDefault(configPath)
    statePath := filepath.Join(tmpDir, "state.json")
    st := state.New(statePath, nil)
    logger := log.NewWithOptions(io.Discard, log.Options{})
    wm := workspace.New(cfg, st, statePath, logger)
    sm := session.New(cfg, st, statePath, wm, logger)
    shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
    defer shutdownCancel()
    server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
    server.SetModelManager(models.New(cfg, nil, "", logger))
    defer server.CloseForTest()

    pmStore := lore.NewPendingMergeStore(t.TempDir(), nil)
    server.SetLorePendingMergeStore(pmStore)
    pmStore.Save(&lore.PendingMerge{Repo: "testrepo", Status: lore.PendingMergeStatusReady})

    req := httptest.NewRequest(http.MethodDelete, "/api/lore/testrepo/pending-merge", nil)
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add("repo", "testrepo")
    req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
    rr := httptest.NewRecorder()
    server.handleLorePendingMergeDelete(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", rr.Code)
    }
    if _, err := pmStore.Get("testrepo"); err == nil {
        t.Error("expected PendingMerge to be deleted")
    }
}
```

### 4b. Write implementation

In `handlers_lore.go`:

```go
func (s *Server) handleLorePendingMergeDelete(w http.ResponseWriter, r *http.Request) {
    repoName := chi.URLParam(r, "repo")
    if s.lorePendingMergeStore == nil {
        http.Error(w, "pending merge store not configured", http.StatusServiceUnavailable)
        return
    }
    if err := s.lorePendingMergeStore.Delete(repoName); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (s *Server) handleLorePendingMergePatch(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
    repoName := chi.URLParam(r, "repo")
    if s.lorePendingMergeStore == nil {
        http.Error(w, "pending merge store not configured", http.StatusServiceUnavailable)
        return
    }
    var body struct {
        EditedContent *string `json:"edited_content"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, "invalid request body", http.StatusBadRequest)
        return
    }
    if err := s.lorePendingMergeStore.UpdateEditedContent(repoName, body.EditedContent); err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}
```

Register routes in `server.go`:

```go
r.Delete("/pending-merge", s.handleLorePendingMergeDelete)
r.Patch("/pending-merge", s.handleLorePendingMergePatch)
```

### 4b. Verify build

```bash
go build ./cmd/schmux
```

### 4c. Commit

```bash
git commit -m "feat(lore): add DELETE and PATCH /api/lore/{repo}/pending-merge endpoints"
```

---

## Step 5: Add POST /api/lore/{repo}/merge (unified cross-proposal merge)

**Files**: Modify `internal/dashboard/handlers_lore.go`, Modify `internal/dashboard/server.go`

This is the core new endpoint. It collects approved public rules from specified proposals, reads the instruction file from the bare repo, runs one LLM merge, and stores the result as a PendingMerge.

### 5a. Write implementation

In `handlers_lore.go`:

```go
func (s *Server) handleLoreUnifiedMerge(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
    repoName := chi.URLParam(r, "repo")

    if s.loreStore == nil || s.lorePendingMergeStore == nil {
        http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
        return
    }
    if s.loreExecutor == nil {
        http.Error(w, "lore curator not configured (no LLM target)", http.StatusServiceUnavailable)
        return
    }

    // Check for existing merging state
    if existing, err := s.lorePendingMergeStore.Get(repoName); err == nil && existing.Status == lore.PendingMergeStatusMerging {
        http.Error(w, "merge already in progress", http.StatusConflict)
        return
    }

    var body struct {
        Proposals []struct {
            ProposalID string   `json:"proposal_id"`
            RuleIDs    []string `json:"rule_ids"`
        } `json:"proposals"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, "invalid request body", http.StatusBadRequest)
        return
    }
    if len(body.Proposals) == 0 {
        http.Error(w, "no proposals provided", http.StatusBadRequest)
        return
    }

    // Gather approved public rules from all specified proposals
    var allRules []lore.Rule
    var allRuleIDs, allProposalIDs []string
    for _, pg := range body.Proposals {
        proposal, err := s.loreStore.Get(repoName, pg.ProposalID)
        if err != nil {
            http.Error(w, fmt.Sprintf("proposal %s not found", pg.ProposalID), http.StatusNotFound)
            return
        }
        allProposalIDs = append(allProposalIDs, pg.ProposalID)
        for _, rule := range proposal.Rules {
            if rule.Status == lore.RuleApproved && rule.EffectiveLayer() == lore.LayerRepoPublic {
                allRules = append(allRules, rule)
                allRuleIDs = append(allRuleIDs, rule.ID)
            }
        }
    }
    if len(allRules) == 0 {
        http.Error(w, "no approved public rules to merge", http.StatusBadRequest)
        return
    }

    // Find bare repo dir
    var bareDir string
    for _, repoConfig := range s.config.Repos {
        if repoConfig.Name == repoName {
            bareDir = s.config.ResolveBareRepoDir(repoConfig.BarePath)
            break
        }
    }
    if bareDir == "" {
        http.Error(w, "repo bare directory not found", http.StatusNotFound)
        return
    }

    // Create PendingMerge in "merging" state
    pm := &lore.PendingMerge{
        Repo:        repoName,
        Status:      lore.PendingMergeStatusMerging,
        RuleIDs:     allRuleIDs,
        ProposalIDs: allProposalIDs,
        CreatedAt:   time.Now().UTC(),
    }
    if err := s.lorePendingMergeStore.Save(pm); err != nil {
        http.Error(w, "failed to create pending merge", http.StatusInternalServerError)
        return
    }

    // Return 202 immediately
    w.WriteHeader(http.StatusAccepted)
    json.NewEncoder(w).Encode(map[string]string{"status": "merging"})

    // Run merge in background
    executor := s.loreExecutor
    pendingStore := s.lorePendingMergeStore
    instrFiles := s.config.GetLoreInstructionFiles()
    logger := s.logger

    go func() {
        ctx, cancel := context.WithTimeout(s.shutdownCtx, 5*time.Minute)
        defer cancel()

        // Fetch latest from remote
        fetchCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "fetch", "--quiet")
        fetchCmd.Run() // best effort

        // Read current instruction file
        targetFile := "CLAUDE.md"
        if len(instrFiles) > 0 {
            targetFile = instrFiles[0]
        }
        currentContent, err := lore.ReadFileFromRepo(ctx, bareDir, targetFile)
        if err != nil {
            logger.Error("failed to read instruction file from repo", "err", err)
            currentContent = "" // empty file is valid — first-time setup
        }

        // Get base SHA
        shaCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "rev-parse", "HEAD")
        shaOut, _ := shaCmd.Output()
        baseSHA := strings.TrimSpace(string(shaOut))

        // Run LLM merge
        prompt := lore.BuildMergePrompt(currentContent, allRules)
        response, err := executor(ctx, prompt, 5*time.Minute)
        if err != nil {
            pm.Status = lore.PendingMergeStatusError
            pm.Error = fmt.Sprintf("Merge failed: %v", err)
            pendingStore.Save(pm)
            s.BroadcastCuratorEvent(CuratorEvent{
                Repo: repoName, Timestamp: time.Now().UTC(),
                EventType: "lore_merge_complete",
                Raw: json.RawMessage(fmt.Sprintf(`{"status":"error","error":%q}`, pm.Error)),
            })
            return
        }

        result, err := lore.ParseMergeResponse(response)
        if err != nil {
            pm.Status = lore.PendingMergeStatusError
            pm.Error = fmt.Sprintf("Failed to parse merge result: %v", err)
            pendingStore.Save(pm)
            s.BroadcastCuratorEvent(CuratorEvent{
                Repo: repoName, Timestamp: time.Now().UTC(),
                EventType: "lore_merge_complete",
                Raw: json.RawMessage(fmt.Sprintf(`{"status":"error","error":%q}`, pm.Error)),
            })
            return
        }

        // Update PendingMerge to ready
        pm.Status = lore.PendingMergeStatusReady
        pm.BaseSHA = baseSHA
        pm.CurrentContent = currentContent
        pm.MergedContent = result.MergedContent
        pm.Summary = result.Summary
        pm.Error = ""
        pendingStore.Save(pm)

        s.BroadcastCuratorEvent(CuratorEvent{
            Repo: repoName, Timestamp: time.Now().UTC(),
            EventType: "lore_merge_complete",
            Raw: json.RawMessage(fmt.Sprintf(`{"status":"ready","repo":%q}`, repoName)),
        })
        logger.Info("unified merge complete", "repo", repoName, "rules", len(allRules))
    }()
}
```

Register route in `server.go`:

```go
r.Post("/merge", s.handleLoreUnifiedMerge)
```

### 5b. Verify build

```bash
go build ./cmd/schmux
```

### 5c. Commit

```bash
git commit -m "feat(lore): add POST /api/lore/{repo}/merge for unified cross-proposal merge"
```

---

## Step 6: Add POST /api/lore/{repo}/push endpoint

**Files**: Modify `internal/dashboard/handlers_lore.go`, Modify `internal/dashboard/server.go`

### 6a. Write test

**Note**: Add `"time"` to the import block in `handlers_lore_test.go` (needed for `time.Now()`).

Add to `internal/dashboard/handlers_lore_test.go`:

```go
func TestHandleLorePush_Success(t *testing.T) {
    if _, err := exec.LookPath("git"); err != nil {
        t.Skip("git not available")
    }
    tmpDir := t.TempDir()

    // Create a remote git repo (allow push to checked-out branch)
    remoteDir := filepath.Join(tmpDir, "remote")
    os.MkdirAll(remoteDir, 0755)
    runGitHelper(t, remoteDir, "init", "-b", "main")
    runGitHelper(t, remoteDir, "config", "user.email", "test@test.com")
    runGitHelper(t, remoteDir, "config", "user.name", "test")
    runGitHelper(t, remoteDir, "config", "receive.denyCurrentBranch", "ignore")
    os.WriteFile(filepath.Join(remoteDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
    runGitHelper(t, remoteDir, "add", ".")
    runGitHelper(t, remoteDir, "commit", "-m", "initial")

    // Get the initial SHA
    shaCmd := exec.Command("git", "-C", remoteDir, "rev-parse", "HEAD")
    shaOut, _ := shaCmd.Output()
    baseSHA := strings.TrimSpace(string(shaOut))

    // Set up server
    configPath := filepath.Join(tmpDir, "config.json")
    cfg := config.CreateDefault(configPath)
    cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
    cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
    cfg.Repos = []config.Repo{
        {Name: "testrepo", URL: remoteDir, BarePath: "testrepo-push.git"},
    }
    cfg.Save()

    statePath := filepath.Join(tmpDir, "state.json")
    st := state.New(statePath, nil)
    logger := log.NewWithOptions(io.Discard, log.Options{})
    wm := workspace.New(cfg, st, statePath, logger)
    sm := session.New(cfg, st, statePath, wm, logger)
    shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
    defer shutdownCancel()
    server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, ServerOptions{ShutdownCtx: shutdownCtx})
    server.SetModelManager(models.New(cfg, nil, "", logger))
    defer server.CloseForTest()

    // Create bare repo (clone remote)
    bareDir := cfg.ResolveBareRepoDir("testrepo-push.git")
    exec.Command("git", "clone", "--bare", remoteDir, bareDir).Run()

    // Set up PendingMergeStore with a ready merge
    pmStore := lore.NewPendingMergeStore(t.TempDir(), nil)
    server.SetLorePendingMergeStore(pmStore)
    pm := &lore.PendingMerge{
        Repo:           "testrepo",
        Status:         lore.PendingMergeStatusReady,
        BaseSHA:        baseSHA,
        RuleIDs:        []string{"r1"},
        ProposalIDs:    []string{"prop-001"},
        MergedContent:  "# Project\n\n- New rule\n",
        CurrentContent: "# Project\n",
        Summary:        "Added new rule",
        CreatedAt:      time.Now().UTC(),
    }
    pmStore.Save(pm)

    // Set up proposal store with matching approved rule
    loreDir := filepath.Join(tmpDir, "lore-proposals")
    proposalStore := lore.NewProposalStore(loreDir, logger)
    server.SetLoreStore(proposalStore)
    proposalStore.Save(&lore.Proposal{
        ID: "prop-001", Repo: "testrepo", Status: lore.ProposalPending,
        Rules: []lore.Rule{{ID: "r1", Text: "New rule", Status: lore.RuleApproved, SuggestedLayer: lore.LayerRepoPublic}},
    })

    // Call push endpoint
    req := httptest.NewRequest(http.MethodPost, "/api/lore/testrepo/push", nil)
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add("repo", "testrepo")
    req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
    rr := httptest.NewRecorder()
    server.handleLorePush(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
    }

    var resp map[string]string
    json.NewDecoder(rr.Body).Decode(&resp)
    if resp["commit_sha"] == "" {
        t.Error("expected commit_sha in response")
    }

    // Verify PendingMerge was cleaned up
    if _, err := pmStore.Get("testrepo"); err == nil {
        t.Error("expected PendingMerge to be deleted after push")
    }
}
```

### 6b. Run test to verify it fails

```bash
go test ./internal/dashboard/ -run TestHandleLorePush -count=1
```

### 6c. Write implementation

In `handlers_lore.go`:

```go
func (s *Server) handleLorePush(w http.ResponseWriter, r *http.Request) {
    repoName := chi.URLParam(r, "repo")

    if s.lorePendingMergeStore == nil || s.loreStore == nil {
        http.Error(w, "lore system not enabled", http.StatusServiceUnavailable)
        return
    }

    pm, err := s.lorePendingMergeStore.Get(repoName)
    if err != nil {
        http.Error(w, "no pending merge", http.StatusNotFound)
        return
    }
    if pm.Status != lore.PendingMergeStatusReady {
        http.Error(w, fmt.Sprintf("pending merge status is %q, not ready", pm.Status), http.StatusConflict)
        return
    }
    if pm.IsExpired() {
        http.Error(w, "pending merge is expired (older than 24h) — re-merge needed", http.StatusGone)
        return
    }

    // Server-side rule validation: verify all rules are still approved
    for _, proposalID := range pm.ProposalIDs {
        proposal, err := s.loreStore.Get(repoName, proposalID)
        if err != nil {
            http.Error(w, fmt.Sprintf("proposal %s not found — rules may have changed", proposalID), http.StatusConflict)
            return
        }
        for _, ruleID := range pm.RuleIDs {
            for _, rule := range proposal.Rules {
                if rule.ID == ruleID && rule.Status != lore.RuleApproved {
                    http.Error(w, "rules changed since merge — re-merge needed", http.StatusConflict)
                    return
                }
            }
        }
    }

    // Find bare repo
    var bareDir string
    var repoURL string
    for _, repoConfig := range s.config.Repos {
        if repoConfig.Name == repoName {
            bareDir = s.config.ResolveBareRepoDir(repoConfig.BarePath)
            repoURL = repoConfig.URL
            break
        }
    }
    if bareDir == "" {
        http.Error(w, "repo not found", http.StatusNotFound)
        return
    }

    ctx := r.Context()

    // Compute target file once — used for both freshness check and write
    instrFiles := s.config.GetLoreInstructionFiles()
    targetFile := "CLAUDE.md"
    if len(instrFiles) > 0 {
        targetFile = instrFiles[0]
    }

    // Fetch latest
    exec.CommandContext(ctx, "git", "-C", bareDir, "fetch", "--quiet").Run()

    // Freshness check
    shaCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "rev-parse", "HEAD")
    shaOut, _ := shaCmd.Output()
    currentSHA := strings.TrimSpace(string(shaOut))

    if currentSHA != pm.BaseSHA {
        currentContent, _ := lore.ReadFileFromRepo(ctx, bareDir, targetFile)
        if currentContent != pm.CurrentContent {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusConflict)
            json.NewEncoder(w).Encode(map[string]string{
                "reason":  "stale",
                "message": fmt.Sprintf("%s has changed since this merge was computed", targetFile),
            })
            return
        }
        // File unchanged, update base SHA
        pm.BaseSHA = currentSHA
        s.lorePendingMergeStore.Save(pm)
    }

    // Determine default branch
    defaultBranch := "main"
    branchCmd := exec.CommandContext(ctx, "git", "-C", bareDir, "symbolic-ref", "HEAD")
    if branchOut, err := branchCmd.Output(); err == nil {
        ref := strings.TrimSpace(string(branchOut))
        defaultBranch = strings.TrimPrefix(ref, "refs/heads/")
    }

    // Create temporary worktree
    worktreeDir := filepath.Join(os.TempDir(), fmt.Sprintf("lore-push-%s-%d", repoName, time.Now().UnixNano()))
    cloneCmd := exec.CommandContext(ctx, "git", "clone", "--single-branch", "-b", defaultBranch, repoURL, worktreeDir)
    if out, err := cloneCmd.CombinedOutput(); err != nil {
        http.Error(w, fmt.Sprintf("clone failed: %s: %s", err, out), http.StatusInternalServerError)
        return
    }
    defer os.RemoveAll(worktreeDir)

    // Write merged content
    content := pm.EffectiveContent()
    fullPath := filepath.Join(worktreeDir, targetFile)
    os.MkdirAll(filepath.Dir(fullPath), 0755)
    if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
        http.Error(w, fmt.Sprintf("write failed: %v", err), http.StatusInternalServerError)
        return
    }

    // Stage, commit, push
    exec.CommandContext(ctx, "git", "-C", worktreeDir, "add", targetFile).Run()

    ruleCount := len(pm.RuleIDs)
    commitMsg := fmt.Sprintf("lore: add %d rules from agent learnings", ruleCount)
    commitCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "commit", "-m", commitMsg)
    commitCmd.Env = append(os.Environ(),
        "GIT_AUTHOR_NAME=schmux",
        "GIT_AUTHOR_EMAIL=schmux@localhost",
        "GIT_COMMITTER_NAME=schmux",
        "GIT_COMMITTER_EMAIL=schmux@localhost",
    )
    if out, err := commitCmd.CombinedOutput(); err != nil {
        http.Error(w, fmt.Sprintf("commit failed: %s: %s", err, out), http.StatusInternalServerError)
        return
    }

    // Get commit SHA
    revParseCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "rev-parse", "HEAD")
    revOut, _ := revParseCmd.Output()
    commitSHA := strings.TrimSpace(string(revOut))

    // Push
    mode := "direct_push"
    if s.config != nil && s.config.Lore != nil {
        mode = s.config.Lore.GetPublicRuleMode()
    }
    if mode == "create_pr" {
        branch := fmt.Sprintf("lore/rules-%s", time.Now().Format("2006-01-02"))
        exec.CommandContext(ctx, "git", "-C", worktreeDir, "checkout", "-b", branch).Run()
        pushCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "push", "-u", "origin", branch)
        if out, err := pushCmd.CombinedOutput(); err != nil {
            http.Error(w, fmt.Sprintf("push to branch failed: %s: %s", err, out), http.StatusInternalServerError)
            return
        }
    } else {
        pushCmd := exec.CommandContext(ctx, "git", "-C", worktreeDir, "push", "origin", "HEAD:"+defaultBranch)
        if out, err := pushCmd.CombinedOutput(); err != nil {
            http.Error(w, fmt.Sprintf("push failed: %s: %s", err, out), http.StatusInternalServerError)
            return
        }
    }

    // Clean up: delete PendingMerge, mark rules as applied
    s.lorePendingMergeStore.Delete(repoName)

    now := time.Now().UTC()
    for _, proposalID := range pm.ProposalIDs {
        proposal, err := s.loreStore.Get(repoName, proposalID)
        if err != nil {
            continue
        }
        for i := range proposal.Rules {
            if proposal.Rules[i].Status == lore.RuleApproved {
                proposal.Rules[i].MergedAt = &now
            }
        }
        if proposal.AllRulesResolved() {
            proposal.Status = lore.ProposalApplied
        }
        s.loreStore.Save(proposal)
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "status":     "pushed",
        "commit_sha": commitSHA,
    })
}
```

Register in `server.go`:

```go
r.Post("/push", s.handleLorePush)
```

### 6d. Run test to verify it passes

```bash
go test ./internal/dashboard/ -run TestHandleLorePush -count=1
```

### 6e. Commit

```bash
git commit -m "feat(lore): add POST /api/lore/{repo}/push with freshness check and temp worktree"
```

---

## Step 7: Add frontend API functions for new endpoints

**File**: Modify `assets/dashboard/src/lib/api.ts`

### 7a. Write implementation

Add after the existing lore API functions (around line 1086):

```typescript
export async function getLorePendingMerge(repoName: string) {
  const resp = await apiFetch(`/api/lore/${repoName}/pending-merge`);
  if (resp.status === 404) return null;
  if (!resp.ok) throw await parseErrorResponse(resp, 'Failed to fetch pending merge');
  return resp.json();
}

export async function startLoreUnifiedMerge(
  repoName: string,
  proposals: { proposal_id: string; rule_ids: string[] }[]
) {
  const resp = await apiFetch(`/api/lore/${repoName}/merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ proposals }),
  });
  if (!resp.ok) throw await parseErrorResponse(resp, 'Failed to start merge');
  return resp.json();
}

export async function pushLoreMerge(repoName: string) {
  const resp = await apiFetch(`/api/lore/${repoName}/push`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!resp.ok) throw await parseErrorResponse(resp, 'Failed to push');
  return resp.json();
}

export async function updateLorePendingMerge(repoName: string, editedContent: string) {
  const resp = await apiFetch(`/api/lore/${repoName}/pending-merge`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ edited_content: editedContent }),
  });
  if (!resp.ok) throw await parseErrorResponse(resp, 'Failed to save edits');
  return resp.json();
}

export async function deleteLorePendingMerge(repoName: string) {
  const resp = await apiFetch(`/api/lore/${repoName}/pending-merge`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!resp.ok) throw await parseErrorResponse(resp, 'Failed to delete pending merge');
  return resp.json();
}
```

### 7b. Verify build

```bash
go run ./cmd/build-dashboard
```

### 7c. Commit

```bash
git commit -m "feat(lore): add frontend API functions for unified merge endpoints"
```

---

## Step 8: Wire PendingMerge invalidation into rule update/dismiss handlers

**Files**: Modify `internal/dashboard/handlers_lore.go`

When a rule is unapproved, edited, or dismissed after a merge is computed, the PendingMerge must be invalidated so the user doesn't see a stale diff or push content that includes a dismissed rule.

### 8a. Write implementation

In `handleLoreRuleUpdate` (around the existing `s.loreStore.UpdateRule(...)` call), add after the successful update:

```go
// Invalidate any pending merge that includes this rule
if s.lorePendingMergeStore != nil {
    s.lorePendingMergeStore.InvalidateIfContainsRule(repoName, ruleID)
}
```

In `handleLoreDismiss` (after `s.loreStore.UpdateStatus(...)` succeeds), add:

```go
// Invalidate any pending merge that includes rules from this proposal
if s.lorePendingMergeStore != nil {
    for _, rule := range proposal.Rules {
        s.lorePendingMergeStore.InvalidateIfContainsRule(repoName, rule.ID)
    }
}
```

### 8b. Verify build

```bash
go build ./cmd/schmux
```

### 8c. Commit

```bash
git commit -m "feat(lore): invalidate PendingMerge when included rules change"
```

---

## Step 9: Add PendingMerge TypeScript type and load pending merges in LorePage

**Files**: Modify `assets/dashboard/src/lib/types.ts`, Modify `assets/dashboard/src/routes/LorePage.tsx`

### 9a. Write implementation

Add to `assets/dashboard/src/lib/types.ts`:

```typescript
export interface PendingMerge {
  repo: string;
  status: 'merging' | 'ready' | 'error';
  base_sha: string;
  rule_ids: string[];
  proposal_ids: string[];
  merged_content: string;
  current_content: string;
  summary: string;
  edited_content?: string;
  error?: string;
  created_at: string;
}
```

In `LorePage.tsx`:

1. **Import** `PendingMerge` from types and new API functions
2. **Replace** `mergePreviews` / `editedPreviews` state with `pendingMerges: Record<string, PendingMerge>`
3. **Add to `loadData`**: fetch pending merge per repo alongside cards
4. **Derive phase** from server state:

```typescript
const derivePhase = (): Phase => {
  for (const pm of Object.values(pendingMerges)) {
    if (pm.status === 'merging') return 'applying';
    if (pm.status === 'ready' || pm.status === 'error') return 'mergeReview';
  }
  // ... existing triage/summary logic
};
```

### 9b. Verify build

```bash
go run ./cmd/build-dashboard
```

### 9c. Commit

```bash
git commit -m "feat(lore): add PendingMerge type and server-driven phase derivation"
```

---

## Step 10: Rewrite handleApply and handleCommitAndPush

**File**: Modify `assets/dashboard/src/routes/LorePage.tsx`

### 10a. Write implementation

**Replace `handleApply`**: Call `POST /api/lore/{repo}/merge` with all approved public rules grouped by proposal. Private layers still use `applyLoreMerge` per-proposal. Remove the old `pollForMergeCompletion` function.

```typescript
const handleApply = async () => {
  // Apply private layers immediately per-proposal (unchanged)
  // ...

  // For public layers: group approved rules by (repo, proposal), call unified merge
  for (const repoName of reposWithPublicRules) {
    const proposals = groupApprovedPublicRulesByProposal(repoName);
    await startLoreUnifiedMerge(repoName, proposals);
  }
  // Phase transitions automatically when pending merge state is set
  await loadData();
};
```

**Replace `handleCommitAndPush`**: Now per-repo, calls push endpoint, handles 409 stale:

```typescript
const handleCommitAndPush = async (repoName: string) => {
  setApplying(true);
  try {
    const result = await pushLoreMerge(repoName);
    const mode = config?.lore?.public_rule_mode || 'direct_push';
    toastSuccess(mode === 'create_pr' ? 'PR created' : `Pushed to ${repoName}`);
    setPendingMerges((prev) => {
      const next = { ...prev };
      delete next[repoName];
      return next;
    });
    invalidateProposals();
  } catch (err) {
    const msg = getErrorMessage(err, 'Push failed');
    if (msg.includes('stale')) {
      setStaleMergeRepo(repoName); // show re-merge banner
    } else {
      await alert('Push Failed', msg);
    }
  } finally {
    setApplying(false);
  }
};
```

### 10b. Verify build

```bash
go run ./cmd/build-dashboard
```

### 10c. Commit

```bash
git commit -m "feat(lore): rewrite handleApply and handleCommitAndPush for unified merge"
```

---

## Step 11: Add Diff/Edit toggle and WebSocket listener

**File**: Modify `assets/dashboard/src/routes/LorePage.tsx`

### 11a. Write implementation

**Diff/Edit toggle** in the `mergeReview` phase:

- Add `activeTab: 'diff' | 'edit'` state (default `'diff'`)
- Diff tab: read-only `ReactDiffViewer` showing `current_content` vs `edited_content ?? merged_content`. "Commit & Push" button.
- Edit tab: `<textarea>` pre-filled with `edited_content ?? merged_content`. "Review Diff" button (switches to diff tab). Debounced save via `updateLorePendingMerge` (~1s after last keystroke).
- Render one card per repo in `pendingMerges` with `status === 'ready'`

**WebSocket listener**: Listen for `lore_merge_complete` events (via existing `CurationContext` or a `useEffect` on the dashboard WebSocket). When received, re-fetch `getLorePendingMerge` for the relevant repo.

### 11b. Verify build

```bash
go run ./cmd/build-dashboard
```

### 11c. Commit

```bash
git commit -m "feat(lore): add Diff/Edit toggle with edit persistence and WebSocket listener"
```

---

## Step 12: Remove old per-proposal merge/apply-merge code paths

**Files**: Modify `internal/dashboard/handlers_lore.go`, Modify `internal/dashboard/server.go`, Modify `internal/lore/proposals.go`

### 12a. Write implementation

1. **Remove `handleLoreMerge`** (the old per-proposal merge handler, lines 297-408)
2. **Remove `finishMerge`** (lines 411-423)
3. **Remove the `schmux/lore` workspace logic** from `handleLoreApplyMerge` — the `case lore.LayerRepoPublic` block (lines 464-601). Replace with an error: `http.Error(w, "public layer is now handled via /merge and /push endpoints", http.StatusGone)`
4. **Remove route registration** for `POST /proposals/{proposalID}/merge` from `server.go`
5. **Remove `MergePreviews` and `MergeError`** fields from the `Proposal` struct in `proposals.go` (lines 127-128) and the `MergePreview` type
6. **Remove `ProposalMerging`** status constant if no longer used
7. **Update frontend**: Remove `startLoreMerge`, `applyLoreMerge` imports/usage from `LorePage.tsx`. Remove `LoreMergePreview` type from `types.ts`. Keep `applyLoreMerge` in `api.ts` since it's still used for private layer apply.

### 12b. Update tests

Update `handlers_lore_test.go`:

- Remove or rewrite `TestHandleLoreApplyMerge_RepoPublic_WorkspaceBased` — this test is for the removed flow
- Remove `TestHandleLoreApplyMerge_RepoPublic_ConflictWhenDirty` — dirty workspace concept is removed
- Keep `TestHandleLoreApplyMergeAutoCommit` but rewrite it to use the new push endpoint flow
- Ensure private layer apply-merge tests still pass (if any)

### 12c. Verify all tests pass

```bash
./test.sh --quick
```

### 12d. Commit

```bash
git commit -m "refactor(lore): remove per-proposal merge and schmux/lore workspace code paths"
```

---

## Step 13: Add orphan worktree cleanup on daemon startup

**File**: Modify `internal/daemon/daemon.go`, in `Daemon.Run()` (around line 360, after home dir is resolved)

### 13a. Write implementation

Add a cleanup function called during daemon startup:

```go
func cleanupOrphanedLoreWorktrees() {
    pattern := filepath.Join(os.TempDir(), "lore-push-*")
    matches, err := filepath.Glob(pattern)
    if err != nil {
        return
    }
    for _, dir := range matches {
        os.RemoveAll(dir)
    }
}
```

Call it early in `Daemon.Run()`, after the home directory is resolved (around line 360).

### 13b. Verify build

```bash
go build ./cmd/schmux
```

### 13c. Commit

```bash
git commit -m "fix(lore): clean up orphaned lore-push worktrees on daemon startup"
```

---

## Step 14: Update docs/api.md

**File**: Modify `docs/api.md`

### 14a. Write implementation

Add documentation for the new endpoints:

- `GET /api/lore/{repo}/pending-merge` — 200 with PendingMerge or 404
- `POST /api/lore/{repo}/merge` — 202, request body with proposals array
- `POST /api/lore/{repo}/push` — 200 with commit_sha, 409 with stale reason, 410 if expired
- `PATCH /api/lore/{repo}/pending-merge` — 200, request body with edited_content
- `DELETE /api/lore/{repo}/pending-merge` — 200

Remove documentation for:

- `POST /api/lore/{repo}/proposals/{id}/merge`
- The `auto_commit` parameter from `POST /api/lore/{repo}/proposals/{id}/apply-merge`

### 14b. Commit

```bash
git commit -m "docs(api): update lore endpoints for unified merge flow"
```

---

## Step 15: End-to-end verification

### 15a. Run full test suite

```bash
./test.sh
```

### 15b. Manual smoke test

1. Start the daemon: `./schmux start`
2. Open dashboard, navigate to `/lore`
3. With pending rules, approve some as public, click Apply
4. Verify: single merge spinner → single diff review
5. Edit the merged content via Edit tab, verify it persists across page refresh
6. Click "Commit & Push" on Diff tab, verify push succeeds
7. Verify PendingMerge is gone on reload

---

## Task Dependencies

| Group | Steps         | Can Parallelize                | Notes                                     |
| ----- | ------------- | ------------------------------ | ----------------------------------------- |
| 1     | Step 1        | No                             | Foundation — PendingMergeStore with mutex |
| 2     | Steps 2, 3, 4 | Yes (independent routes)       | All depend on Step 1                      |
| 3     | Steps 5, 6    | No (6 depends on 5's pattern)  | Core backend logic                        |
| 4     | Steps 7, 8    | Yes (FE API + BE invalidation) | Depend on Group 3                         |
| 5     | Step 9        | No (depends on Step 7)         | FE: types + phase derivation              |
| 6     | Step 10       | No (depends on Step 9)         | FE: apply + push handlers                 |
| 7     | Step 11       | No (depends on Step 10)        | FE: diff/edit toggle + WS                 |
| 8     | Step 12       | No (depends on Step 11)        | Remove old code                           |
| 9     | Steps 13, 14  | Yes (independent)              | Cleanup and docs                          |
| 10    | Step 15       | No (depends on all)            | E2E verification                          |
