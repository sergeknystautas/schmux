# Plan: Communication Styles Feature

**Goal**: Add a "communication style" system orthogonal to personas — styles define how agents talk (voice/tone) while personas define what they do (role/approach). Users set per-agent-type defaults in global config, with per-session overrides at spawn.

**Architecture**: New `internal/style/` package mirrors `internal/persona/`. Styles are YAML files in `~/.schmux/styles/`. At spawn time, persona + style are composed into a single markdown system prompt and injected via the existing adapter mechanism. 25 built-in styles ship embedded in the binary.

**Tech Stack**: Go (backend), React/TypeScript (dashboard), YAML (style storage), Vitest (frontend tests)

**Design doc**: `docs/specs/comm-styles-design-final.md`

---

## Dependency Table

| Group | Steps      | Can Parallelize | Notes                                                       |
| ----- | ---------- | --------------- | ----------------------------------------------------------- |
| 1     | Steps 1-3  | Yes             | Independent packages: style domain, contracts, config       |
| 2     | Step 4     | No              | Handlers depend on Group 1                                  |
| 3     | Steps 5-6  | Yes             | Session manager + spawn handler depend on Group 2           |
| 4     | Step 7     | No              | Gen-types depends on contracts from Group 1                 |
| 5     | Steps 8-12 | Yes             | UI pages are independent of each other, depend on gen-types |
| 6     | Step 13    | No              | Spawn page depends on API client from Group 5               |
| 7     | Step 14    | No              | Config page depends on API client                           |
| 8     | Step 15    | No              | Remote support depends on session manager                   |
| 9     | Step 16    | No              | End-to-end verification                                     |

---

## Step 1: Style domain package — parse and marshal

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

```bash
git add internal/style/parse.go internal/style/parse_test.go
git commit -m "feat(style): add Style struct and YAML parse/marshal"
```

---

## Step 2: Style domain package — manager with CRUD

**Files**: `internal/style/manager.go` (create), `internal/style/builtins.go` (create), `internal/style/manager_test.go` (create)

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
	if len(styles) != 25 {
		t.Errorf("expected 25 built-in styles, got %d", len(styles))
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

Then create all 25 built-in YAML files in `internal/style/builtins/`. Each follows this pattern:

```yaml
---
id: pirate
name: Pirate
icon: '🏴‍☠️'
tagline: Speaks like a swashbuckling sea captain
built_in: true
---
Adopt the communication style of a pirate. Use nautical metaphors,
sprinkle in "arrr", "ahoy", and "matey" naturally. Refer to bugs
as "barnacles", problems as "rough seas", and successes as
"plundered treasure". Address the user as "captain" or "matey".

Your technical output (code, commands, file paths) must remain
accurate and unmodified — the style applies to your natural
language communication only, never to code or tool invocations.
```

Create one `.yaml` file per style for all 25 (see roster in design doc).

### 2d. Run tests

```bash
go test ./internal/style/ -v
```

### 2e. Commit

```bash
git add internal/style/
git commit -m "feat(style): add manager, builtins, and 25 built-in communication styles"
```

---

## Step 3: API contracts

**Files**: `internal/api/contracts/style.go` (create)

### 3a. No test needed (pure data types, tested transitively via handlers)

### 3b. Write implementation

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

### 3c. Commit

```bash
git add internal/api/contracts/style.go
git commit -m "feat(contracts): add Style API types"
```

---

## Step 4: Dashboard CRUD handlers + routes

**Files**: `internal/dashboard/handlers_styles.go` (create), `internal/dashboard/server.go` (modify)

### 4a. Write implementation

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

**Modify `internal/dashboard/server.go`:**

1. Add `styleManager *style.Manager` field next to `personaManager` (~line 215)
2. Initialize it after persona manager (~line 344):
   ```go
   stylesDir := filepath.Join(filepath.Dir(statePath), "styles")
   s.styleManager = style.NewManager(stylesDir)
   if err := s.styleManager.EnsureBuiltins(); err != nil {
       logger.Warn("failed to ensure built-in styles", "err", err)
   }
   ```
3. Register routes after persona routes (~line 708):
   ```go
   // Style routes
   r.Get("/styles", s.handleListStyles)
   r.Post("/styles", s.handleCreateStyle)
   r.Get("/styles/{id}", s.handleGetStyle)
   r.Put("/styles/{id}", s.handleUpdateStyle)
   r.Delete("/styles/{id}", s.handleDeleteStyle)
   ```
4. Add import for `"github.com/sergeknystautas/schmux/internal/style"`

### 4b. Verify it compiles

```bash
go build ./cmd/schmux
```

### 4c. Commit

```bash
git add internal/dashboard/handlers_styles.go internal/dashboard/server.go
git commit -m "feat(dashboard): add style CRUD handlers and routes"
```

---

## Step 5: Config — add `comm_styles` field

**Files**: `internal/config/config.go` (modify), `internal/api/contracts/config.go` (modify)

### 5a. Modify config struct

Add to `Config` struct in `internal/config/config.go`:

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

Add to `ConfigUpdateRequest` in `internal/api/contracts/config.go`:

```go
CommStyles *map[string]string `json:"comm_styles,omitempty"`
```

Add to `ConfigResponse` in `internal/api/contracts/config.go`:

```go
CommStyles map[string]string `json:"comm_styles,omitempty"`
```

Wire up the update handler in `handleConfigUpdate` to apply `CommStyles` when non-nil.

### 5b. Verify

```bash
go test ./internal/config/ -v
go build ./cmd/schmux
```

### 5c. Commit

```bash
git add internal/config/config.go internal/api/contracts/config.go internal/dashboard/handlers_config.go
git commit -m "feat(config): add comm_styles per-agent-type defaults"
```

---

## Step 6: Session state — add `StyleID`

**Files**: `internal/state/state.go` (modify), `internal/session/manager.go` (modify)

### 6a. Add `StyleID` to session state

In `internal/state/state.go`, add to `Session` struct (~line 326, after `PersonaID`):

```go
StyleID   string `json:"style_id,omitempty"`
```

### 6b. Add `StyleID` to `SpawnOptions`

In `internal/session/manager.go`, add to `SpawnOptions` struct (~line 663, after `PersonaPrompt`):

```go
StyleID string
```

Update the session creation (~line 881) to include `StyleID`:

```go
sess := state.Session{
    ID:          sessionID,
    WorkspaceID: w.ID,
    Target:      opts.TargetName,
    Nickname:    uniqueNickname,
    PersonaID:   opts.PersonaID,
    StyleID:     opts.StyleID,
    TmuxSession: tmuxSession,
    CreatedAt:   time.Now(),
    Pid:         pid,
}
```

### 6c. Verify

```bash
go build ./cmd/schmux
```

### 6d. Commit

```bash
git add internal/state/state.go internal/session/manager.go
git commit -m "feat(state): add StyleID to Session and SpawnOptions"
```

---

## Step 7: Spawn handler — style resolution and prompt composition

**Files**: `internal/dashboard/handlers_spawn.go` (modify)

### 7a. Modify SpawnRequest

Add after `PersonaID` field (~line 44):

```go
StyleID   string `json:"style_id,omitempty"` // optional: communication style override ("none" to suppress global default)
```

### 7b. Rename `formatPersonaPrompt` to `formatAgentSystemPrompt`

Replace the function at ~line 818:

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

### 7c. Update persona resolution block (~line 295)

Replace the persona resolution and spawn loop to include style:

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

### 7d. Rename the persona file path

In `internal/session/manager.go` (~line 829), rename the file:

```go
personaFilePath := filepath.Join(state.SchmuxDataDir(w.Path), fmt.Sprintf("system-prompt-%s.md", sessionID))
```

### 7e. Verify

```bash
go build ./cmd/schmux
go test ./internal/dashboard/ -v
```

### 7f. Commit

```bash
git add internal/dashboard/handlers_spawn.go internal/session/manager.go
git commit -m "feat(spawn): compose persona + style into unified system prompt"
```

---

## Step 8: Gen-types — add style types

**File**: `cmd/gen-types/main.go` (modify)

### 8a. Add style types to rootTypes

After the persona lines (~line 39), add:

```go
reflect.TypeOf(contracts.StyleListResponse{}),
reflect.TypeOf(contracts.StyleCreateRequest{}),
reflect.TypeOf(contracts.StyleUpdateRequest{}),
```

### 8b. Regenerate

```bash
go run ./cmd/gen-types
```

### 8c. Commit

```bash
git add cmd/gen-types/main.go assets/dashboard/src/lib/types.generated.ts
git commit -m "feat(gen-types): add Style types to TypeScript generation"
```

---

## Step 9: UI — StyleForm component

**File**: `assets/dashboard/src/components/StyleForm.tsx` (create)

### 9a. Write implementation

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

### 9b. Commit

```bash
git add assets/dashboard/src/components/StyleForm.tsx
git commit -m "feat(ui): add StyleForm component"
```

---

## Step 10: UI — API client functions

**File**: `assets/dashboard/src/lib/api.ts` (modify)

### 10a. Add style API functions

Add after the persona functions (~line 1206):

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

### 10b. Commit

```bash
git add assets/dashboard/src/lib/api.ts
git commit -m "feat(ui): add style API client functions"
```

---

## Step 11: UI — Styles list, create, and edit pages

**Files**: `assets/dashboard/src/routes/StylesListPage.tsx` (create), `assets/dashboard/src/routes/StyleCreatePage.tsx` (create), `assets/dashboard/src/routes/StyleEditPage.tsx` (create)

### 11a. Write implementations

Mirror the persona pages, adapting for style fields (no color, add tagline). `StylesListPage` shows cards with `icon`, `name`, and `tagline` instead of prompt preview. Cards do not have the color accent bar. Otherwise identical patterns to `PersonasListPage`, `PersonaCreatePage`, `PersonaEditPage`.

### 11b. Commit

```bash
git add assets/dashboard/src/routes/StylesListPage.tsx assets/dashboard/src/routes/StyleCreatePage.tsx assets/dashboard/src/routes/StyleEditPage.tsx
git commit -m "feat(ui): add Styles list, create, and edit pages"
```

---

## Step 12: UI — Routes and sidebar nav

**Files**: `assets/dashboard/src/App.tsx` (modify), `assets/dashboard/src/components/ToolsSection.tsx` (modify)

### 12a. Add routes to App.tsx

After the persona routes (~line 83):

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

### 12b. Add sidebar entry to ToolsSection.tsx

Add a "Comm Styles" entry in the `menuItems` array (~line 117), after the Personas entry:

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

### 12c. Commit

```bash
git add assets/dashboard/src/App.tsx assets/dashboard/src/components/ToolsSection.tsx
git commit -m "feat(ui): add styles routes and sidebar navigation"
```

---

## Step 13: UI — Spawn page style dropdown

**File**: `assets/dashboard/src/routes/SpawnPage.tsx` (modify)

### 13a. Add state and fetch

Add alongside persona state (~line 169):

```tsx
const [styles, setStyles] = useState<Style[]>([]);
const [selectedStyleId, setSelectedStyleId] = useState('');
```

Fetch styles alongside personas (~line 275):

```tsx
getStyles()
  .then((data) => setStyles(data.styles || []))
  .catch(() => {});
```

### 13b. Move persona + style to own row

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

### 13c. Pass `style_id` to spawn request

In all three spawn handlers, add `style_id` to the request body:

```tsx
style_id: selectedStyleId || undefined,
```

### 13d. Update tests

Update `SpawnPage.agent-select.test.tsx` — change assertions that look for `persona-select` inside `agent-repo-row` to look inside `persona-style-row` instead.

### 13e. Commit

```bash
git add assets/dashboard/src/routes/SpawnPage.tsx assets/dashboard/src/routes/SpawnPage.agent-select.test.tsx
git commit -m "feat(ui): add style dropdown to spawn wizard"
```

---

## Step 14: UI — Config page comm styles section

**File**: `assets/dashboard/src/routes/ConfigPage.tsx` (modify)

### 14a. Add comm styles section

Add a "Communication Styles" section to the config page. For each configured agent type (from the tools/targets config), show a dropdown to select a default style. The dropdown options come from `getStyles()`. The selection updates `comm_styles` in the config via `updateConfig()`.

### 14b. Commit

```bash
git add assets/dashboard/src/routes/ConfigPage.tsx
git commit -m "feat(ui): add per-agent-type style defaults to config page"
```

---

## Step 15: Remote session support

**Files**: `internal/session/manager.go` (modify), `internal/dashboard/handlers_spawn.go` (modify)

### 15a. Add RemoteSpawnOptions struct

In `internal/session/manager.go`, near `SpawnOptions`:

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

### 15b. Refactor SpawnRemote signature

Change `SpawnRemote` to accept `RemoteSpawnOptions`:

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

Store `PersonaID` and `StyleID` on the remote session state.

### 15c. Update spawn handler

Change the `SpawnRemote` call in `handlers_spawn.go` (~line 353):

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

### 15d. Verify

```bash
go build ./cmd/schmux
go test ./internal/session/ -v
go test ./internal/dashboard/ -v
```

### 15e. Commit

```bash
git add internal/session/manager.go internal/dashboard/handlers_spawn.go
git commit -m "feat(remote): add persona + style support to remote sessions"
```

---

## Step 16: End-to-end verification

### 16a. Run full test suite

```bash
./test.sh
```

### 16b. Build dashboard

```bash
go run ./cmd/build-dashboard
```

### 16c. Build binary

```bash
go build ./cmd/schmux
```

### 16d. Manual verification checklist

- [ ] Start daemon: `./schmux start`
- [ ] Visit `/styles` — verify 25 built-in styles appear
- [ ] Create a custom style — verify it appears in the list
- [ ] Edit a built-in style — verify changes save
- [ ] Delete a built-in style — verify it resets to default
- [ ] Visit `/spawn` — verify style dropdown appears below agent/repo row
- [ ] Spawn a session with a style — verify the agent uses the style
- [ ] Set a global default in `/config` — verify spawn uses it when no override
- [ ] Override with "None" at spawn — verify no style is applied
- [ ] Spawn a remote session with a style — verify it works

### 16e. Final commit

```bash
git commit -m "feat(comm-styles): complete communication styles feature"
```
