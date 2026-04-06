# Plan: Communication Styles Feature (v2)

**Goal**: Add a "communication style" system orthogonal to personas -- styles define how agents talk (voice/tone) while personas define what they do (role/approach). Users set per-agent-type defaults in global config, with per-session overrides at spawn.

**Architecture**: New `internal/style/` package mirrors `internal/persona/`. Styles are YAML files in `~/.schmux/styles/`. At spawn time, persona + style are composed into a single markdown system prompt and injected via the existing adapter mechanism. 25 built-in styles ship embedded in the binary.

**Tech Stack**: Go (backend), React/TypeScript (dashboard), YAML (style storage), Vitest (frontend tests)

**Design doc**: `docs/specs/comm-styles-design-final.md`

---

## Changes from previous version

1. **All commit steps now use `/commit` instead of `git commit`.** CLAUDE.md requires `/commit` for every commit; it enforces `./test.sh`, `docs/api.md` checks, and self-assessment.

2. **Added `docs/api.md` update step (new Step 8).** The new `/api/styles` endpoints, the `style_id` spawn parameter, and the `comm_styles` config field must all be documented. CI enforces this via `scripts/check-api-docs.sh`.

3. **Added handler tests for CRUD endpoints (Step 5).** Tests cover validation logic (reserved IDs, slug format, required fields), branching behavior (reset vs. delete for built-ins), and happy paths. Follows the `newTestServer` + `httptest` pattern from `handlers_spawn_test.go`.

4. **Completed Step 6 config wiring with exact code blocks.** Shows the `handleConfigUpdate` addition (applying `CommStyles` when non-nil) and the `handleGetConfig` addition (returning `CommStyles` in the response), following the `EnabledModels` pattern.

5. **Split old Step 2 into two steps.** Step 2 is now manager infrastructure + 3 sample styles. Step 3 is the remaining 22 built-in styles. This keeps each step under 5 minutes.

6. **Noted backward compatibility for file rename.** The rename from `.schmux/persona-{sessionID}.md` to `.schmux/system-prompt-{sessionID}.md` is safe because files are consumed at spawn time and never re-read.

7. **Verified `SpawnRemote` callers.** Only one call site exists (`handlers_spawn.go:353`); no other callers need updating.

---

## Dependency Table

| Group | Steps       | Can Parallelize | Notes                                                                                 |
| ----- | ----------- | --------------- | ------------------------------------------------------------------------------------- |
| 1     | Steps 1-4   | Yes             | Independent packages: style parse, manager infrastructure, sample builtins, contracts |
| 2     | Step 3      | After 2         | Remaining built-in styles depend on manager infrastructure                            |
| 3     | Steps 5-6   | No              | Handlers + handler tests depend on Group 1                                            |
| 4     | Steps 7-8   | Partially       | Session state + config wiring are independent; spawn handler depends on both          |
| 5     | Step 8      | After 6         | docs/api.md update depends on handlers being defined                                  |
| 6     | Steps 9-10  | Yes             | Spawn handler + remote support depend on Group 4                                      |
| 7     | Step 11     | No              | Gen-types depends on contracts from Group 1                                           |
| 8     | Steps 12-15 | Yes             | UI pages are independent of each other, depend on gen-types                           |
| 9     | Step 16     | No              | Spawn page depends on API client from Group 8                                         |
| 10    | Step 17     | No              | Config page depends on API client                                                     |
| 11    | Step 18     | No              | End-to-end verification                                                               |

---

## Step 1: Style domain package -- parse and marshal

**Files**: `internal/style/parse.go` (create), `internal/style/parse_test.go` (create)

### 1a. Write tests

```go
// internal/style/parse_test.go
package style

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	input := `---
id: pirate
name: Pirate
icon: "\U0001f3f4\u200d\u2620\ufe0f"
tagline: Speaks like a swashbuckling sea captain
built_in: true
---

Adopt the communication style of a pirate.
`
	s, err := ParseStyle([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ID != "pirate" {
		t.Errorf("ID = %q, want %q", s.ID, "pirate")
	}
	if s.Name != "Pirate" {
		t.Errorf("Name = %q, want %q", s.Name, "Pirate")
	}
	if s.Tagline != "Speaks like a swashbuckling sea captain" {
		t.Errorf("Tagline = %q", s.Tagline)
	}
	if !s.BuiltIn {
		t.Errorf("BuiltIn = false, want true")
	}
	if s.Prompt != "Adopt the communication style of a pirate." {
		t.Errorf("Prompt = %q", s.Prompt)
	}
}

func TestParseMissingDelimiter(t *testing.T) {
	input := `id: pirate
name: Pirate
`
	_, err := ParseStyle([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiters")
	}
}

func TestParseEmptyBody(t *testing.T) {
	input := `---
id: test
name: Test
icon: "T"
tagline: Test style
---
`
	s, err := ParseStyle([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", s.Prompt)
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	original := &Style{
		ID: "test", Name: "Test", Icon: "T",
		Tagline: "A test", Prompt: "Do the thing.",
		BuiltIn: false,
	}
	data, err := MarshalStyle(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	parsed, err := ParseStyle(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if parsed.ID != original.ID || parsed.Name != original.Name || parsed.Prompt != original.Prompt || parsed.Tagline != original.Tagline {
		t.Errorf("roundtrip mismatch: got %+v", parsed)
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/style/ -run TestParse -v
```

### 1c. Write implementation

```go
// internal/style/parse.go
package style

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Style represents a communication style for an agent.
type Style struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Icon    string `yaml:"icon" json:"icon"`
	Tagline string `yaml:"tagline" json:"tagline"`
	BuiltIn bool   `yaml:"built_in" json:"built_in"`
	Prompt  string `yaml:"-" json:"prompt"` // from body, not frontmatter
}

// ParseStyle parses a YAML file with frontmatter metadata and a body prompt.
func ParseStyle(data []byte) (*Style, error) {
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		if strings.HasSuffix(rest, "\n---") {
			idx = len(rest) - 4
		} else if strings.HasSuffix(rest, "\n---\n") {
			idx = len(rest) - 5
		} else {
			return nil, fmt.Errorf("missing closing frontmatter delimiter")
		}
	}

	frontmatter := rest[:idx]
	body := ""
	if idx+4 < len(rest) {
		body = strings.TrimSpace(rest[idx+4:])
	}

	var s Style
	if err := yaml.Unmarshal([]byte(frontmatter), &s); err != nil {
		return nil, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	s.Prompt = body
	return &s, nil
}

// MarshalStyle serializes a Style to YAML frontmatter + body format.
func MarshalStyle(s *Style) ([]byte, error) {
	meta := struct {
		ID      string `yaml:"id"`
		Name    string `yaml:"name"`
		Icon    string `yaml:"icon"`
		Tagline string `yaml:"tagline"`
		BuiltIn bool   `yaml:"built_in"`
	}{
		ID: s.ID, Name: s.Name, Icon: s.Icon,
		Tagline: s.Tagline, BuiltIn: s.BuiltIn,
	}

	fm, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	if s.Prompt != "" {
		buf.WriteString("\n")
		buf.WriteString(s.Prompt)
		buf.WriteString("\n")
	}
	return []byte(buf.String()), nil
}
```

### 1d. Run test to verify it passes

```bash
go test ./internal/style/ -run TestParse -v
go test ./internal/style/ -run TestMarshal -v
```

### 1e. Commit

```
/commit
```

---

## Step 2: Style domain package -- manager infrastructure + sample builtins

**Files**: `internal/style/manager.go` (create), `internal/style/builtins.go` (create), `internal/style/manager_test.go` (create), `internal/style/builtins/pirate.yaml` (create), `internal/style/builtins/caveman.yaml` (create), `internal/style/builtins/butler.yaml` (create)

This step creates the manager with CRUD operations and 3 sample built-in styles to validate the infrastructure. The remaining 22 styles are added in Step 3.

### 2a. Write tests

```go
// internal/style/manager_test.go
package style

import "testing"

func TestManagerList(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	styles, err := mgr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(styles) != 0 {
		t.Errorf("expected empty list, got %d", len(styles))
	}
}

func TestManagerCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{
		ID: "test-style", Name: "Test", Icon: "T",
		Tagline: "A test", Prompt: "Be helpful.", BuiltIn: false,
	}
	if err := mgr.Create(s); err != nil {
		t.Fatalf("create error: %v", err)
	}
	got, err := mgr.Get("test-style")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if got.Name != "Test" || got.Prompt != "Be helpful." || got.Tagline != "A test" {
		t.Errorf("unexpected style: %+v", got)
	}
}

func TestManagerCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{ID: "dup", Name: "Dup", Icon: "D", Tagline: "Dup"}
	if err := mgr.Create(s); err != nil {
		t.Fatalf("first create error: %v", err)
	}
	if err := mgr.Create(s); err == nil {
		t.Fatal("expected error on duplicate create")
	}
}

func TestManagerUpdate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{ID: "upd", Name: "Original", Icon: "O", Tagline: "T"}
	mgr.Create(s)
	s.Name = "Updated"
	if err := mgr.Update(s); err != nil {
		t.Fatalf("update error: %v", err)
	}
	got, _ := mgr.Get("upd")
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want %q", got.Name, "Updated")
	}
}

func TestManagerDelete(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	s := &Style{ID: "del", Name: "Del", Icon: "X", Tagline: "T"}
	mgr.Create(s)
	if err := mgr.Delete("del"); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if _, err := mgr.Get("del"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestManagerGetNotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	_, err := mgr.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing style")
	}
}

func TestEnsureBuiltins(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	if err := mgr.EnsureBuiltins(); err != nil {
		t.Fatalf("ensure error: %v", err)
	}
	styles, err := mgr.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	// Start with 3 sample builtins; updated to 25 in Step 3
	if len(styles) < 3 {
		t.Errorf("expected at least 3 built-in styles, got %d", len(styles))
	}
	for _, s := range styles {
		if !s.BuiltIn {
			t.Errorf("style %q should be built_in", s.ID)
		}
	}
}

func TestEnsureBuiltinsSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureBuiltins()
	s, _ := mgr.Get("pirate")
	s.Prompt = "Custom prompt."
	mgr.Update(s)
	mgr.EnsureBuiltins()
	got, _ := mgr.Get("pirate")
	if got.Prompt != "Custom prompt." {
		t.Error("EnsureBuiltins overwrote user-modified style")
	}
}

func TestResetBuiltIn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	mgr.EnsureBuiltins()
	s, _ := mgr.Get("pirate")
	original := s.Prompt
	s.Prompt = "Modified prompt."
	mgr.Update(s)
	if err := mgr.ResetBuiltIn("pirate"); err != nil {
		t.Fatalf("reset error: %v", err)
	}
	got, _ := mgr.Get("pirate")
	if got.Prompt != original {
		t.Errorf("reset did not restore original prompt")
	}
}

func TestResetBuiltInNotBuiltIn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	err := mgr.ResetBuiltIn("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-built-in style")
	}
}
```

### 2b. Run test to verify it fails

```bash
go test ./internal/style/ -run TestManager -v
go test ./internal/style/ -run TestEnsure -v
go test ./internal/style/ -run TestReset -v
```

### 2c. Write implementation

```go
// internal/style/builtins.go
package style

import "embed"

//go:embed builtins/*.yaml
var builtinsFS embed.FS
```

```go
// internal/style/manager.go
package style

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manager handles CRUD operations on style YAML files.
type Manager struct {
	dir string
}

// NewManager creates a Manager that stores styles in the given directory.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// Dir returns the styles directory path.
func (m *Manager) Dir() string {
	return m.dir
}

// List returns all styles sorted by name.
func (m *Manager) List() ([]*Style, error) {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create styles directory: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read styles directory: %w", err)
	}
	var styles []*Style
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		s, err := ParseStyle(data)
		if err != nil {
			continue
		}
		styles = append(styles, s)
	}
	sort.Slice(styles, func(i, j int) bool {
		return styles[i].Name < styles[j].Name
	})
	return styles, nil
}

// Get returns a style by ID.
func (m *Manager) Get(id string) (*Style, error) {
	path := filepath.Join(m.dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("style not found: %s", id)
		}
		return nil, fmt.Errorf("failed to read style %s: %w", id, err)
	}
	return ParseStyle(data)
}

// Create writes a new style file. Fails if the ID already exists.
func (m *Manager) Create(s *Style) error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create styles directory: %w", err)
	}
	path := filepath.Join(m.dir, s.ID+".yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("style already exists: %s", s.ID)
	}
	data, err := MarshalStyle(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Update overwrites an existing style file. Fails if the ID doesn't exist.
func (m *Manager) Update(s *Style) error {
	path := filepath.Join(m.dir, s.ID+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("style not found: %s", s.ID)
	}
	data, err := MarshalStyle(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Delete removes a style file.
func (m *Manager) Delete(id string) error {
	path := filepath.Join(m.dir, id+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("style not found: %s", id)
	}
	return os.Remove(path)
}

// EnsureBuiltins writes built-in styles to disk if they don't already exist.
func (m *Manager) EnsureBuiltins() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create styles directory: %w", err)
	}
	entries, err := builtinsFS.ReadDir("builtins")
	if err != nil {
		return fmt.Errorf("failed to read embedded builtins: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		destPath := filepath.Join(m.dir, entry.Name())
		if _, err := os.Stat(destPath); err == nil {
			continue
		}
		data, err := builtinsFS.ReadFile("builtins/" + entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read embedded builtin %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write builtin %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// ResetBuiltIn re-copies the original built-in from the embedded filesystem.
func (m *Manager) ResetBuiltIn(id string) error {
	filename := id + ".yaml"
	data, err := builtinsFS.ReadFile("builtins/" + filename)
	if err != nil {
		return fmt.Errorf("not a built-in style: %s", id)
	}
	destPath := filepath.Join(m.dir, filename)
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write builtin %s: %w", filename, err)
	}
	return nil
}
```

Create 3 sample built-in YAML files in `internal/style/builtins/`:

- `pirate.yaml`
- `caveman.yaml`
- `butler.yaml`

Each follows the frontmatter + body format shown in the design doc.

### 2d. Run tests

```bash
go test ./internal/style/ -v
```

### 2e. Commit

```
/commit
```

---

## Step 3: Remaining built-in styles (22 of 25)

**Files**: `internal/style/builtins/*.yaml` (create 22 files), `internal/style/manager_test.go` (modify)

### 3a. Create remaining YAML files

Create one `.yaml` file per style for the remaining 22 styles from the roster in the design doc:

**Archetypes**: toddler, film-noir, surfer, shakespeare, corporate, cowboy, valley-girl

**Famous People / Characters**: trump, queen-elizabeth, werner-herzog, david-attenborough, homer-simpson, mr-t, yoda, borat, gordon-ramsay, samuel-l-jackson, morgan-freeman, bob-ross, snoop-dogg, batman, christopher-walken

Each follows the same frontmatter + body pattern. Every prompt includes the guardrail: "Your technical output (code, commands, file paths) must remain accurate and unmodified -- the style applies to your natural language communication only, never to code or tool invocations."

### 3b. Update test count

Update the `TestEnsureBuiltins` assertion in `manager_test.go` to check for exactly 25 styles:

```go
if len(styles) != 25 {
    t.Errorf("expected 25 built-in styles, got %d", len(styles))
}
```

### 3c. Run tests

```bash
go test ./internal/style/ -v
```

### 3d. Commit

```
/commit
```

---

## Step 4: API contracts

**Files**: `internal/api/contracts/style.go` (create)

### 4a. No test needed (pure data types, tested transitively via handlers)

### 4b. Write implementation

```go
// internal/api/contracts/style.go
package contracts

// Style represents a communication style for an agent.
type Style struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Tagline string `json:"tagline"`
	Prompt  string `json:"prompt"`
	BuiltIn bool   `json:"built_in"`
}

// StyleCreateRequest is the body for POST /api/styles.
type StyleCreateRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Tagline string `json:"tagline"`
	Prompt  string `json:"prompt"`
}

// StyleUpdateRequest is the body for PUT /api/styles/{id}.
type StyleUpdateRequest struct {
	Name    *string `json:"name,omitempty"`
	Icon    *string `json:"icon,omitempty"`
	Tagline *string `json:"tagline,omitempty"`
	Prompt  *string `json:"prompt,omitempty"`
}

// StyleListResponse is the body for GET /api/styles.
type StyleListResponse struct {
	Styles []Style `json:"styles"`
}
```

### 4c. Commit

```
/commit
```

---

## Step 5: Dashboard CRUD handlers, routes, and handler tests

**Files**: `internal/dashboard/handlers_styles.go` (create), `internal/dashboard/handlers_styles_test.go` (create), `internal/dashboard/server.go` (modify)

### 5a. Write handler implementation

```go
// internal/dashboard/handlers_styles.go
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/style"
)

var validStyleID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

func (s *Server) handleListStyles(w http.ResponseWriter, r *http.Request) {
	styles, err := s.styleManager.List()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list styles: %v", err), http.StatusInternalServerError)
		return
	}
	response := contracts.StyleListResponse{
		Styles: make([]contracts.Style, len(styles)),
	}
	for i, st := range styles {
		response.Styles[i] = styleToContract(st)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "list-styles", "err", err)
	}
}

func (s *Server) handleGetStyle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st, err := s.styleManager.Get(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("style not found: %s", id), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(styleToContract(st)); err != nil {
		s.logger.Error("failed to encode response", "handler", "get-style", "err", err)
	}
}

func (s *Server) handleCreateStyle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req contracts.StyleCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Name == "" || req.Icon == "" || req.Prompt == "" {
		http.Error(w, "id, name, icon, and prompt are required", http.StatusBadRequest)
		return
	}
	if !validStyleID.MatchString(req.ID) {
		http.Error(w, "id must be a URL-safe slug (lowercase alphanumeric + hyphens)", http.StatusBadRequest)
		return
	}
	if req.ID == "create" || req.ID == "none" {
		http.Error(w, fmt.Sprintf("%q is a reserved ID", req.ID), http.StatusBadRequest)
		return
	}
	st := &style.Style{
		ID: req.ID, Name: req.Name, Icon: req.Icon,
		Tagline: req.Tagline, Prompt: req.Prompt, BuiltIn: false,
	}
	if err := s.styleManager.Create(st); err != nil {
		http.Error(w, fmt.Sprintf("failed to create style: %v", err), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(styleToContract(st)); err != nil {
		s.logger.Error("failed to encode response", "handler", "create-style", "err", err)
	}
}

func (s *Server) handleUpdateStyle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	id := chi.URLParam(r, "id")
	existing, err := s.styleManager.Get(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("style not found: %s", id), http.StatusNotFound)
		return
	}
	var req contracts.StyleUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Icon != nil {
		existing.Icon = *req.Icon
	}
	if req.Tagline != nil {
		existing.Tagline = *req.Tagline
	}
	if req.Prompt != nil {
		existing.Prompt = *req.Prompt
	}
	if err := s.styleManager.Update(existing); err != nil {
		http.Error(w, fmt.Sprintf("failed to update style: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(styleToContract(existing)); err != nil {
		s.logger.Error("failed to encode response", "handler", "update-style", "err", err)
	}
}

func (s *Server) handleDeleteStyle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.styleManager.Get(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("style not found: %s", id), http.StatusNotFound)
		return
	}
	if existing.BuiltIn {
		if err := s.styleManager.ResetBuiltIn(id); err != nil {
			http.Error(w, fmt.Sprintf("failed to reset style: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		if err := s.styleManager.Delete(id); err != nil {
			http.Error(w, fmt.Sprintf("failed to delete style: %v", err), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func styleToContract(s *style.Style) contracts.Style {
	return contracts.Style{
		ID: s.ID, Name: s.Name, Icon: s.Icon,
		Tagline: s.Tagline, Prompt: s.Prompt, BuiltIn: s.BuiltIn,
	}
}
```

### 5b. Modify `internal/dashboard/server.go`

1. Add `styleManager *style.Manager` field next to `personaManager` (search for `personaManager`)
2. Initialize it after persona manager initialization (search for `personaManager = persona.NewManager`):
   ```go
   stylesDir := filepath.Join(filepath.Dir(statePath), "styles")
   s.styleManager = style.NewManager(stylesDir)
   if err := s.styleManager.EnsureBuiltins(); err != nil {
       logger.Warn("failed to ensure built-in styles", "err", err)
   }
   ```
3. Register routes after persona routes (search for `r.Delete("/personas/{id}"`):
   ```go
   // Style routes
   r.Get("/styles", s.handleListStyles)
   r.Post("/styles", s.handleCreateStyle)
   r.Get("/styles/{id}", s.handleGetStyle)
   r.Put("/styles/{id}", s.handleUpdateStyle)
   r.Delete("/styles/{id}", s.handleDeleteStyle)
   ```
4. Add import for `"github.com/sergeknystautas/schmux/internal/style"`

### 5c. Write handler tests

```go
// internal/dashboard/handlers_styles_test.go
package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// Helper to make requests with chi URL params.
func styleRequest(t *testing.T, method, path string, body interface{}) *http.Request {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(
		r.Context(),
	)
}

func TestHandleListStyles(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/styles", nil)
	rr := httptest.NewRecorder()
	server.handleListStyles(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp contracts.StyleListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	// Should have built-in styles
	if len(resp.Styles) == 0 {
		t.Error("expected at least one style from builtins")
	}
}

func TestHandleCreateStyle_Validation(t *testing.T) {
	tests := []struct {
		name       string
		body       contracts.StyleCreateRequest
		wantCode   int
		wantSubstr string
	}{
		{
			name:       "missing required fields",
			body:       contracts.StyleCreateRequest{ID: "test"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "id, name, icon, and prompt are required",
		},
		{
			name:       "invalid slug format - uppercase",
			body:       contracts.StyleCreateRequest{ID: "Bad-ID", Name: "Test", Icon: "T", Prompt: "p"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "URL-safe slug",
		},
		{
			name:       "invalid slug format - leading hyphen",
			body:       contracts.StyleCreateRequest{ID: "-bad", Name: "Test", Icon: "T", Prompt: "p"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "URL-safe slug",
		},
		{
			name:       "reserved ID - none",
			body:       contracts.StyleCreateRequest{ID: "none", Name: "None", Icon: "X", Prompt: "p"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "reserved ID",
		},
		{
			name:       "reserved ID - create",
			body:       contracts.StyleCreateRequest{ID: "create", Name: "Create", Icon: "C", Prompt: "p"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "reserved ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := newTestServer(t)
			req := styleRequest(t, "POST", "/api/styles", tt.body)
			rr := httptest.NewRecorder()
			server.handleCreateStyle(rr, req)

			if rr.Code != tt.wantCode {
				t.Errorf("got status %d, want %d; body: %s", rr.Code, tt.wantCode, rr.Body.String())
			}
			if tt.wantSubstr != "" {
				if !bytes.Contains(rr.Body.Bytes(), []byte(tt.wantSubstr)) {
					t.Errorf("response %q should contain %q", rr.Body.String(), tt.wantSubstr)
				}
			}
		})
	}
}

func TestHandleCreateStyle_Success(t *testing.T) {
	server, _, _ := newTestServer(t)
	body := contracts.StyleCreateRequest{
		ID: "test-style", Name: "Test Style", Icon: "T",
		Tagline: "A test", Prompt: "Be helpful.",
	}
	req := styleRequest(t, "POST", "/api/styles", body)
	rr := httptest.NewRecorder()
	server.handleCreateStyle(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rr.Code, rr.Body.String())
	}
	var resp contracts.Style
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.ID != "test-style" || resp.Name != "Test Style" || resp.BuiltIn {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestHandleCreateStyle_Duplicate(t *testing.T) {
	server, _, _ := newTestServer(t)
	body := contracts.StyleCreateRequest{
		ID: "dup-style", Name: "Dup", Icon: "D", Prompt: "p",
	}
	req1 := styleRequest(t, "POST", "/api/styles", body)
	rr1 := httptest.NewRecorder()
	server.handleCreateStyle(rr1, req1)
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create failed: %d", rr1.Code)
	}

	req2 := styleRequest(t, "POST", "/api/styles", body)
	rr2 := httptest.NewRecorder()
	server.handleCreateStyle(rr2, req2)
	if rr2.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr2.Code)
	}
}

func TestHandleDeleteStyle_BuiltInResets(t *testing.T) {
	server, _, _ := newTestServer(t)

	// The server initializes builtins in NewServer. Get the pirate style
	// via chi router context.
	r := chi.NewRouter()
	r.Delete("/api/styles/{id}", server.handleDeleteStyle)
	r.Get("/api/styles/{id}", server.handleGetStyle)

	// First, verify pirate exists
	getReq := httptest.NewRequest("GET", "/api/styles/pirate", nil)
	getRR := httptest.NewRecorder()
	r.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("pirate not found: %d", getRR.Code)
	}

	// Update pirate to have custom content
	var original contracts.Style
	json.NewDecoder(getRR.Body).Decode(&original)

	// Delete (should reset, not remove)
	delReq := httptest.NewRequest("DELETE", "/api/styles/pirate", nil)
	delRR := httptest.NewRecorder()
	r.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRR.Code)
	}

	// Verify pirate still exists (was reset, not deleted)
	getReq2 := httptest.NewRequest("GET", "/api/styles/pirate", nil)
	getRR2 := httptest.NewRecorder()
	r.ServeHTTP(getRR2, getReq2)
	if getRR2.Code != http.StatusOK {
		t.Errorf("pirate should still exist after reset, got %d", getRR2.Code)
	}
}

func TestHandleDeleteStyle_CustomDeletes(t *testing.T) {
	server, _, _ := newTestServer(t)

	r := chi.NewRouter()
	r.Post("/api/styles", server.handleCreateStyle)
	r.Delete("/api/styles/{id}", server.handleDeleteStyle)
	r.Get("/api/styles/{id}", server.handleGetStyle)

	// Create a custom style
	body := contracts.StyleCreateRequest{
		ID: "custom-del", Name: "Custom", Icon: "C", Prompt: "p",
	}
	data, _ := json.Marshal(body)
	createReq := httptest.NewRequest("POST", "/api/styles", bytes.NewReader(data))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	r.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", createRR.Code)
	}

	// Delete it
	delReq := httptest.NewRequest("DELETE", "/api/styles/custom-del", nil)
	delRR := httptest.NewRecorder()
	r.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRR.Code)
	}

	// Verify it's gone
	getReq := httptest.NewRequest("GET", "/api/styles/custom-del", nil)
	getRR := httptest.NewRecorder()
	r.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", getRR.Code)
	}
}

func TestHandleDeleteStyle_NotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	r := chi.NewRouter()
	r.Delete("/api/styles/{id}", server.handleDeleteStyle)

	req := httptest.NewRequest("DELETE", "/api/styles/nonexistent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}
```

### 5d. Verify

```bash
go build ./cmd/schmux
go test ./internal/dashboard/ -run TestHandleListStyles -v
go test ./internal/dashboard/ -run TestHandleCreateStyle -v
go test ./internal/dashboard/ -run TestHandleDeleteStyle -v
```

### 5e. Commit

```
/commit
```

---

## Step 6: Config -- add `comm_styles` field with full handler wiring

**Files**: `internal/config/config.go` (modify), `internal/api/contracts/config.go` (modify), `internal/dashboard/handlers_config.go` (modify)

### 6a. Modify config struct

Add to `Config` struct in `internal/config/config.go` (search for `EnabledModels` to find the right neighborhood):

```go
CommStyles map[string]string `json:"comm_styles,omitempty"`
```

Add getter method:

```go
func (c *Config) GetCommStyles() map[string]string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if c.CommStyles == nil {
        return map[string]string{}
    }
    // Return a copy to avoid race conditions
    result := make(map[string]string, len(c.CommStyles))
    for k, v := range c.CommStyles {
        result[k] = v
    }
    return result
}
```

### 6b. Modify contracts

Add to `ConfigUpdateRequest` in `internal/api/contracts/config.go` (search for `EnabledModels`):

```go
CommStyles *map[string]string `json:"comm_styles,omitempty"`
```

Add to `ConfigResponse` in `internal/api/contracts/config.go` (search for `EnabledModels`):

```go
CommStyles map[string]string `json:"comm_styles,omitempty"`
```

### 6c. Wire `handleGetConfig` in `internal/dashboard/handlers_config.go`

In the `handleGetConfig` function, add to the `response := contracts.ConfigResponse{...}` struct literal, after `EnabledModels` (search for `EnabledModels:` in `handleGetConfig`, which is at approximately line 103):

```go
CommStyles:                 s.config.GetCommStyles(),
```

### 6d. Wire `handleConfigUpdate` in `internal/dashboard/handlers_config.go`

In the `handleConfigUpdate` function, add after the `EnabledModels` block (search for `if req.EnabledModels != nil`, which is at approximately line 697):

```go
if req.CommStyles != nil {
    cfg.CommStyles = *req.CommStyles
}
```

### 6e. Verify

```bash
go test ./internal/config/ -v
go build ./cmd/schmux
```

### 6f. Commit

```
/commit
```

---

## Step 7: Session state -- add `StyleID`

**Files**: `internal/state/state.go` (modify), `internal/session/manager.go` (modify)

### 7a. Add `StyleID` to session state

In `internal/state/state.go`, add to `Session` struct (search for `PersonaID`):

```go
StyleID   string `json:"style_id,omitempty"`
```

### 7b. Add `StyleID` to `SpawnOptions`

In `internal/session/manager.go`, add to `SpawnOptions` struct (search for `PersonaPrompt`):

```go
StyleID string
```

Update the session creation (search for `sess := state.Session{` in the `Spawn` function) to include `StyleID`:

```go
StyleID:     opts.StyleID,
```

### 7c. Verify

```bash
go build ./cmd/schmux
```

### 7d. Commit

```
/commit
```

---

## Step 8: Update `docs/api.md`

**Files**: `docs/api.md` (modify)

This step adds documentation for the new style endpoints, the `style_id` spawn parameter, and the `comm_styles` config field. CI enforces that API package changes are accompanied by `docs/api.md` updates via `scripts/check-api-docs.sh`.

### 8a. Add Styles API section

Add a new "## Styles API" section after the existing "## Personas API" section (after the `DELETE /api/personas/{id}` notes). Follow the same format as the Personas API section:

````markdown
## Styles API

Communication styles define how agents talk (voice, tone, personality) and are orthogonal to personas. Each style is a YAML file with frontmatter metadata and a body containing the style prompt. 25 built-in styles are provided on first run.

### GET /api/styles

Returns all styles sorted by name.

Response:

```json
{
  "styles": [
    {
      "id": "pirate",
      "name": "Pirate",
      "icon": "🏴‍☠️",
      "tagline": "Speaks like a swashbuckling sea captain",
      "prompt": "Adopt the communication style of a pirate...",
      "built_in": true
    }
  ]
}
```
````

### GET /api/styles/{id}

Returns a single style by ID.

Response: a `Style` object (same shape as items in the list response).

Errors:

- 404: "style not found: {id}"

### POST /api/styles

Creates a new custom style.

Request:

```json
{
  "id": "my-style",
  "name": "My Style",
  "icon": "✨",
  "tagline": "A custom style",
  "prompt": "Communicate in a custom way..."
}
```

Response: the created `Style` object (HTTP 201).

Errors:

- 400: missing required fields (id, name, icon, prompt) / invalid slug format / `"create"` or `"none"` are reserved IDs
- 409: "style already exists: {id}"

### PUT /api/styles/{id}

Updates an existing style. All fields are optional; only provided fields are changed.

Request:

```json
{
  "name": "Updated Name",
  "icon": "🎭",
  "tagline": "Updated tagline",
  "prompt": "Updated prompt..."
}
```

Response: the updated `Style` object.

Errors:

- 404: "style not found: {id}"

### DELETE /api/styles/{id}

Deletes a custom style, or resets a built-in style to its default content.

Response: 204 No Content

Errors:

- 404: "style not found: {id}"
- 500: "failed to delete/reset style: ..."

Notes:

- Built-in styles are restored from embedded defaults rather than permanently deleted
- The `style_id` field in `SpawnRequest` references these IDs

```

### 8b. Add `style_id` to SpawnRequest documentation

In the `POST /api/spawn` section, add `"style_id": "optional"` to the request JSON (after `"persona_id": "optional"`).

Add a note after the existing `persona_id` documentation:

```

- `style_id` is optional. When set, the style's prompt is composed with the persona prompt and injected into the agent at spawn time. The special value `"none"` explicitly suppresses the global default style for this session. When absent, the per-agent-type global default from `comm_styles` config is used.

````

### 8c. Add `style_id` to session response

In the session response JSON examples (WebSocket broadcast and GET responses), add `"style_id": "optional"` after `"persona_id"`.

### 8d. Add `comm_styles` to config documentation

In the `GET /api/config` response JSON, add (after `"enabled_models"`):
```json
"comm_styles": { "claude": "pirate", "codex": "caveman" },
````

In the `POST/PUT /api/config` request JSON, add (after `"enabled_models"`):

```json
"comm_styles": { "claude": "pirate", "codex": "caveman" },
```

### 8e. Commit

```
/commit
```

---

## Step 9: Spawn handler -- style resolution and prompt composition

**Files**: `internal/dashboard/handlers_spawn.go` (modify)

### 9a. Modify SpawnRequest

Add after `PersonaID` field (search for `PersonaID`):

```go
StyleID   string `json:"style_id,omitempty"` // optional: communication style override ("none" to suppress global default)
```

### 9b. Rename `formatPersonaPrompt` to `formatAgentSystemPrompt`

Replace the function (search for `func formatPersonaPrompt`):

```go
func formatAgentSystemPrompt(p *persona.Persona, st *style.Style) string {
    var b strings.Builder
    if p != nil {
        fmt.Fprintf(&b, "## Persona: %s\n\n", p.Name)
        if p.Expectations != "" {
            fmt.Fprintf(&b, "### Behavioral Expectations\n%s\n\n", strings.TrimSpace(p.Expectations))
        }
        fmt.Fprintf(&b, "### Instructions\n%s\n", strings.TrimSpace(p.Prompt))
    }
    if st != nil {
        if p != nil {
            b.WriteString("\n---\n\n")
        }
        fmt.Fprintf(&b, "## Communication Style: %s\n\n%s\n", st.Name, strings.TrimSpace(st.Prompt))
    }
    return b.String()
}
```

Update all callers of `formatPersonaPrompt` to use `formatAgentSystemPrompt`. There is one call site (search for `formatPersonaPrompt(` to find it).

### 9c. Update persona resolution block

Replace the persona resolution and spawn loop to include style (search for the `if req.PersonaID != ""` block):

```go
// Resolve persona
var personaObj *persona.Persona
if req.PersonaID != "" {
    p, err := s.personaManager.Get(req.PersonaID)
    if err != nil {
        writeJSONError(w, fmt.Sprintf("persona not found: %s", req.PersonaID), http.StatusBadRequest)
        return
    }
    personaObj = p
}

// Resolve explicit style override
var explicitStyleObj *style.Style
explicitNone := false
if req.StyleID == "none" {
    explicitNone = true
} else if req.StyleID != "" {
    st, err := s.styleManager.Get(req.StyleID)
    if err != nil {
        writeJSONError(w, fmt.Sprintf("style not found: %s", req.StyleID), http.StatusBadRequest)
        return
    }
    explicitStyleObj = st
}
```

Inside the per-target spawn loop, before the spawn call:

```go
// Resolve style for this target
var styleObj *style.Style
if explicitStyleObj != nil {
    styleObj = explicitStyleObj
} else if !explicitNone {
    baseTool := s.models.ResolveTargetToTool(targetName)
    if defaultID := s.config.GetCommStyles()[baseTool]; defaultID != "" {
        styleObj, _ = s.styleManager.Get(defaultID)
    }
}

resolvedStyleID := ""
if styleObj != nil {
    resolvedStyleID = styleObj.ID
}

agentPrompt := formatAgentSystemPrompt(personaObj, styleObj)
```

Update the local spawn call to pass `StyleID` and use `agentPrompt` instead of `personaPrompt`:

```go
sess, err = s.session.Spawn(ctx, session.SpawnOptions{
    // ... existing fields ...
    PersonaID:     req.PersonaID,
    PersonaPrompt: agentPrompt,
    StyleID:       resolvedStyleID,
    // ...
})
```

### 9d. Rename the persona file path

In `internal/session/manager.go` (search for `persona-`), rename the file:

```go
personaFilePath := filepath.Join(state.SchmuxDataDir(w.Path), fmt.Sprintf("system-prompt-%s.md", sessionID))
```

**Backward compatibility note:** This rename is safe. The file is written at spawn time and consumed immediately by the agent. No running sessions re-read this file, so there is no backward compatibility issue.

### 9e. Verify

```bash
go build ./cmd/schmux
go test ./internal/dashboard/ -v
```

### 9f. Commit

```
/commit
```

---

## Step 10: Remote session support

**Files**: `internal/session/manager.go` (modify), `internal/dashboard/handlers_spawn.go` (modify)

### 10a. Add RemoteSpawnOptions struct

In `internal/session/manager.go`, near `SpawnOptions` (search for `type SpawnOptions struct`):

```go
type RemoteSpawnOptions struct {
    ProfileID     string
    FlavorStr     string
    HostID        string
    TargetName    string
    Prompt        string
    Nickname      string
    PersonaID     string
    PersonaPrompt string
    StyleID       string
}
```

### 10b. Refactor SpawnRemote signature

Change `SpawnRemote` to accept `RemoteSpawnOptions` (search for `func (m *Manager) SpawnRemote`):

```go
func (m *Manager) SpawnRemote(ctx context.Context, opts RemoteSpawnOptions) (*state.Session, error)
```

Update the function body to use `opts.ProfileID`, `opts.FlavorStr`, etc. instead of positional params.

Add persona+style inline injection after command construction:

```go
if opts.PersonaPrompt != "" {
    if adapter := detect.GetAdapter(baseTool); adapter != nil {
        if adapter.PersonaInjection() == detect.PersonaCLIFlag {
            command = fmt.Sprintf("%s --append-system-prompt %s",
                command, shellutil.Quote(opts.PersonaPrompt))
        }
    }
}
```

Store `PersonaID` and `StyleID` on the remote session state:

```go
sess := state.Session{
    // ... existing fields ...
    PersonaID:   opts.PersonaID,
    StyleID:     opts.StyleID,
    // ...
}
```

**Callers:** Only one call site exists (`handlers_spawn.go:353`). Verified via grep -- no CLI code or tests call `SpawnRemote` directly.

### 10c. Update spawn handler

Change the `SpawnRemote` call in `handlers_spawn.go` (search for `s.session.SpawnRemote`):

```go
sess, err = s.session.SpawnRemote(ctx, session.RemoteSpawnOptions{
    ProfileID:     req.RemoteProfileID,
    FlavorStr:     req.RemoteFlavor,
    HostID:        remoteHostID,
    TargetName:    targetName,
    Prompt:        req.Prompt,
    Nickname:      nickname,
    PersonaID:     req.PersonaID,
    PersonaPrompt: agentPrompt,
    StyleID:       resolvedStyleID,
})
```

### 10d. Verify

```bash
go build ./cmd/schmux
go test ./internal/session/ -v
go test ./internal/dashboard/ -v
```

### 10e. Commit

```
/commit
```

---

## Step 11: Gen-types -- add style types

**File**: `cmd/gen-types/main.go` (modify)

### 11a. Add style types to rootTypes

After the persona lines (search for `PersonaListResponse` in `rootTypes`), add:

```go
reflect.TypeOf(contracts.StyleListResponse{}),
reflect.TypeOf(contracts.StyleCreateRequest{}),
reflect.TypeOf(contracts.StyleUpdateRequest{}),
```

### 11b. Regenerate

```bash
go run ./cmd/gen-types
```

### 11c. Commit

```
/commit
```

---

## Step 12: UI -- StyleForm component

**File**: `assets/dashboard/src/components/StyleForm.tsx` (create)

### 12a. Write implementation

Mirror `PersonaForm.tsx` but with `tagline` instead of `color`/`expectations`:

```tsx
// assets/dashboard/src/components/StyleForm.tsx
import { useState } from 'react';

export interface StyleFormData {
  id: string;
  name: string;
  icon: string;
  tagline: string;
  prompt: string;
}

export const emptyForm: StyleFormData = {
  id: '',
  name: '',
  icon: '',
  tagline: '',
  prompt: '',
};

export function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

interface StyleFormProps {
  mode: 'create' | 'edit';
  initialData?: StyleFormData;
  saving: boolean;
  onSave: (data: StyleFormData) => void;
  onCancel: () => void;
}

export default function StyleForm({ mode, initialData, saving, onSave, onCancel }: StyleFormProps) {
  const [formData, setFormData] = useState<StyleFormData>(initialData ?? emptyForm);
  const [autoSlug, setAutoSlug] = useState(mode === 'create');

  const handleNameChange = (name: string) => {
    setFormData((prev) => ({
      ...prev,
      name,
      ...(autoSlug && mode === 'create' ? { id: slugify(name) } : {}),
    }));
  };

  return (
    <div className="persona-form" data-testid="style-form">
      <div className="form-row">
        <div className="form-group flex-1">
          <label className="form-group__label" htmlFor="style-name">
            Name
          </label>
          <input
            id="style-name"
            type="text"
            className="input"
            value={formData.name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder="Pirate"
          />
        </div>
        <div className="form-group" style={{ flex: '0 0 auto', minWidth: 0 }}>
          <label className="form-group__label" htmlFor="style-icon">
            Icon (emoji)
          </label>
          <input
            id="style-icon"
            type="text"
            className="input"
            value={formData.icon}
            onChange={(e) => setFormData((prev) => ({ ...prev, icon: e.target.value }))}
            placeholder="🏴‍☠️"
            style={{ width: '5rem', textAlign: 'center', fontSize: '1.2rem' }}
          />
        </div>
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="style-tagline">
          Tagline
        </label>
        <span
          className="form-group__hint"
          style={{ marginTop: 0, marginBottom: '4px', display: 'block' }}
        >
          A short description of this communication style
        </span>
        <input
          id="style-tagline"
          type="text"
          className="input"
          value={formData.tagline}
          onChange={(e) => setFormData((prev) => ({ ...prev, tagline: e.target.value }))}
          placeholder="Speaks like a swashbuckling sea captain"
        />
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="style-prompt">
          Style Prompt
        </label>
        <span
          className="form-group__hint"
          style={{ marginTop: 0, marginBottom: '4px', display: 'block' }}
        >
          Instructions for how the agent should communicate
        </span>
        <textarea
          id="style-prompt"
          className="textarea"
          style={{ fontFamily: 'var(--font-mono)' }}
          value={formData.prompt}
          onChange={(e) => setFormData((prev) => ({ ...prev, prompt: e.target.value }))}
          rows={15}
          placeholder="Adopt the communication style of a pirate..."
        />
      </div>

      {mode === 'create' && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-group__label" htmlFor="style-id">
              ID (slug)
            </label>
            <input
              id="style-id"
              type="text"
              className="input"
              value={formData.id}
              onChange={(e) => {
                setAutoSlug(false);
                setFormData((prev) => ({ ...prev, id: e.target.value }));
              }}
              placeholder="pirate"
            />
          </div>
        </div>
      )}

      <div className="form-actions">
        <button className="btn" onClick={onCancel}>
          Cancel
        </button>
        <button className="btn btn--primary" onClick={() => onSave(formData)} disabled={saving}>
          {saving ? 'Saving...' : mode === 'create' ? 'Create' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}
```

### 12b. Commit

```
/commit
```

---

## Step 13: UI -- API client functions

**File**: `assets/dashboard/src/lib/api.ts` (modify)

### 13a. Add style API functions

Add after the persona functions (search for `deletePersona`):

```typescript
export async function getStyles(): Promise<StyleListResponse> {
  const response = await apiFetch('/api/styles');
  if (!response.ok) await parseErrorResponse(response, 'Failed to fetch styles');
  return response.json();
}

export async function createStyle(req: StyleCreateRequest): Promise<Style> {
  const response = await apiFetch('/api/styles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to create style');
  return response.json();
}

export async function updateStyle(id: string, req: StyleUpdateRequest): Promise<Style> {
  const response = await apiFetch(`/api/styles/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(req),
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to update style');
  return response.json();
}

export async function deleteStyle(id: string): Promise<void> {
  const response = await apiFetch(`/api/styles/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) await parseErrorResponse(response, 'Failed to delete style');
}
```

Add the necessary type imports from `types.generated` (they will exist after gen-types runs).

### 13b. Commit

```
/commit
```

---

## Step 14: UI -- Styles list, create, and edit pages

**Files**: `assets/dashboard/src/routes/StylesListPage.tsx` (create), `assets/dashboard/src/routes/StyleCreatePage.tsx` (create), `assets/dashboard/src/routes/StyleEditPage.tsx` (create)

### 14a. Write implementations

Mirror the persona pages, adapting for style fields (no color, add tagline). `StylesListPage` shows cards with `icon`, `name`, and `tagline` instead of prompt preview. Cards do not have the color accent bar. Otherwise identical patterns to `PersonasListPage`, `PersonaCreatePage`, `PersonaEditPage`.

### 14b. Commit

```
/commit
```

---

## Step 15: UI -- Routes and sidebar nav

**Files**: `assets/dashboard/src/App.tsx` (modify), `assets/dashboard/src/components/ToolsSection.tsx` (modify)

### 15a. Add routes to App.tsx

After the persona routes (search for `PersonaEditPage`):

```tsx
<Route path="/styles" element={<StylesListPage />} />
<Route path="/styles/create" element={<StyleCreatePage />} />
<Route path="/styles/:styleId" element={<StyleEditPage />} />
```

Add imports at the top:

```tsx
import StylesListPage from './routes/StylesListPage';
import StyleCreatePage from './routes/StyleCreatePage';
import StyleEditPage from './routes/StyleEditPage';
```

### 15b. Add sidebar entry to ToolsSection.tsx

Add a "Comm Styles" entry in the `menuItems` array (search for `Personas` in `menuItems`), after the Personas entry:

```tsx
{
  to: '/styles',
  label: 'Comm Styles',
  icon: (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
    </svg>
  ),
},
```

### 15c. Commit

```
/commit
```

---

## Step 16: UI -- Spawn page style dropdown

**File**: `assets/dashboard/src/routes/SpawnPage.tsx` (modify)

### 16a. Add state and fetch

Add alongside persona state (search for `selectedPersonaId`):

```tsx
const [styles, setStyles] = useState<Style[]>([]);
const [selectedStyleId, setSelectedStyleId] = useState('');
```

Fetch styles alongside personas (search for `getPersonas`):

```tsx
getStyles()
  .then((data) => setStyles(data.styles || []))
  .catch(() => {});
```

### 16b. Move persona + style to own row

In all three spawn modes, move the persona `<select>` out of `agent-repo-row` and into a new `persona-style-row` div with the style dropdown:

```tsx
{
  (personas.length > 0 || styles.length > 0) && (
    <div className="form-row" data-testid="persona-style-row">
      {personas.length > 0 && (
        <select
          data-testid="persona-select"
          className="select flex-1"
          value={selectedPersonaId}
          onChange={(e) => setSelectedPersonaId(e.target.value)}
        >
          <option value="">No Persona</option>
          {personas.map((p) => (
            <option key={p.id} value={p.id}>
              {p.icon} {p.name}
            </option>
          ))}
        </select>
      )}
      {styles.length > 0 && (
        <select
          data-testid="style-select"
          className="select flex-1"
          value={selectedStyleId}
          onChange={(e) => setSelectedStyleId(e.target.value)}
        >
          <option value="">Global Default</option>
          <option value="none">None</option>
          {styles.map((s) => (
            <option key={s.id} value={s.id}>
              {s.icon} {s.name}
            </option>
          ))}
        </select>
      )}
    </div>
  );
}
```

### 16c. Pass `style_id` to spawn request

In all three spawn handlers, add `style_id` to the request body:

```tsx
style_id: selectedStyleId || undefined,
```

### 16d. Update tests

Update `SpawnPage.agent-select.test.tsx` -- change assertions that look for `persona-select` inside `agent-repo-row` to look inside `persona-style-row` instead.

### 16e. Commit

```
/commit
```

---

## Step 17: UI -- Config page comm styles section

**File**: `assets/dashboard/src/routes/ConfigPage.tsx` (modify)

### 17a. Add comm styles section

Add a "Communication Styles" section to the config page. For each configured agent type (from the tools/targets config), show a dropdown to select a default style. The dropdown options come from `getStyles()`. The selection updates `comm_styles` in the config via `updateConfig()`.

### 17b. Commit

```
/commit
```

---

## Step 18: End-to-end verification

### 18a. Run full test suite

```bash
./test.sh
```

### 18b. Build dashboard

```bash
go run ./cmd/build-dashboard
```

### 18c. Build binary

```bash
go build ./cmd/schmux
```

### 18d. Manual verification checklist

- [ ] Start daemon: `./schmux start`
- [ ] Visit `/styles` -- verify 25 built-in styles appear
- [ ] Create a custom style -- verify it appears in the list
- [ ] Edit a built-in style -- verify changes save
- [ ] Delete a built-in style -- verify it resets to default
- [ ] Delete a custom style -- verify it is removed
- [ ] Visit `/spawn` -- verify style dropdown appears below agent/repo row
- [ ] Spawn a session with a style -- verify the agent uses the style
- [ ] Set a global default in `/config` -- verify spawn uses it when no override
- [ ] Override with "None" at spawn -- verify no style is applied
- [ ] Spawn a remote session with a style -- verify it works

### 18e. Final commit

```
/commit
```
