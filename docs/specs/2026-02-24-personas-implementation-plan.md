# Personas Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Add persona support — named behavioral profiles (system prompts + visual identity) that shape how agents operate, managed via dashboard UI and injected at spawn time.

**Architecture:** Personas are YAML files with frontmatter (metadata) and body (system prompt) stored in `~/.schmux/personas/`. A new `internal/persona/` package handles CRUD and built-in embedding. The persona prompt is injected via each agent's most authoritative mechanism (`--append-system-prompt-file` for Claude, instruction files for Codex/Gemini). The dashboard gets a `/personas` management page and persona selection in the spawn wizard.

**Tech Stack:** Go (backend, YAML parsing, embed), React/TypeScript (dashboard), Vitest + React Testing Library (frontend tests)

**Design spec:** `docs/specs/personas.md`

---

### Task 1: YAML Frontmatter Parser

**Files:**

- Create: `internal/persona/parse.go`
- Test: `internal/persona/parse_test.go`

**Step 1: Write the failing test**

Create `internal/persona/parse_test.go`:

```go
package persona

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	input := `---
id: security-auditor
name: Security Auditor
icon: "\U0001f512"
color: "#e74c3c"
expectations: |
  Produce a structured report.
built_in: true
---

You are a security expert.
Check for vulnerabilities.
`
	p, err := ParsePersona([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "security-auditor" {
		t.Errorf("ID = %q, want %q", p.ID, "security-auditor")
	}
	if p.Name != "Security Auditor" {
		t.Errorf("Name = %q, want %q", p.Name, "Security Auditor")
	}
	if p.Color != "#e74c3c" {
		t.Errorf("Color = %q, want %q", p.Color, "#e74c3c")
	}
	if p.BuiltIn != true {
		t.Errorf("BuiltIn = %v, want true", p.BuiltIn)
	}
	expectedPrompt := "You are a security expert.\nCheck for vulnerabilities."
	if p.Prompt != expectedPrompt {
		t.Errorf("Prompt = %q, want %q", p.Prompt, expectedPrompt)
	}
}

func TestParseFrontmatterMissingDelimiter(t *testing.T) {
	input := `id: security-auditor
name: Security Auditor
`
	_, err := ParsePersona([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiters")
	}
}

func TestParseFrontmatterEmptyBody(t *testing.T) {
	input := `---
id: test
name: Test
icon: "T"
color: "#000"
---
`
	p, err := ParsePersona([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", p.Prompt)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/persona/...
```

Expected: FAIL — `ParsePersona` not defined.

**Step 3: Write minimal implementation**

Create `internal/persona/parse.go`:

```go
package persona

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Persona represents a behavioral profile for an agent.
type Persona struct {
	ID           string `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Icon         string `yaml:"icon" json:"icon"`
	Color        string `yaml:"color" json:"color"`
	Expectations string `yaml:"expectations" json:"expectations"`
	BuiltIn      bool   `yaml:"built_in" json:"built_in"`
	Prompt       string `yaml:"-" json:"prompt"` // from body, not frontmatter
}

// ParsePersona parses a YAML file with frontmatter metadata and a body prompt.
func ParsePersona(data []byte) (*Persona, error) {
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	// Find closing delimiter
	rest := content[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		// Check for closing delimiter at end of file
		if strings.HasSuffix(rest, "\n---") {
			idx = len(rest) - 4
		} else {
			return nil, fmt.Errorf("missing closing frontmatter delimiter")
		}
	}

	frontmatter := rest[:idx]
	body := ""
	if idx+4 < len(rest) {
		body = strings.TrimSpace(rest[idx+4:])
	}

	var p Persona
	if err := yaml.Unmarshal([]byte(frontmatter), &p); err != nil {
		return nil, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	p.Prompt = body
	return &p, nil
}

// MarshalPersona serializes a Persona to YAML frontmatter + body format.
func MarshalPersona(p *Persona) ([]byte, error) {
	// Marshal frontmatter (without prompt)
	meta := struct {
		ID           string `yaml:"id"`
		Name         string `yaml:"name"`
		Icon         string `yaml:"icon"`
		Color        string `yaml:"color"`
		Expectations string `yaml:"expectations,omitempty"`
		BuiltIn      bool   `yaml:"built_in"`
	}{
		ID: p.ID, Name: p.Name, Icon: p.Icon,
		Color: p.Color, Expectations: p.Expectations, BuiltIn: p.BuiltIn,
	}

	fm, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	if p.Prompt != "" {
		buf.WriteString("\n")
		buf.WriteString(p.Prompt)
		buf.WriteString("\n")
	}
	return []byte(buf.String()), nil
}
```

Note: Check if `gopkg.in/yaml.v3` is already a dependency. If not, run `go get gopkg.in/yaml.v3`. The project may use a different YAML library — check `go.mod`.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/persona/...
```

Expected: PASS

**Step 5: Add roundtrip test for MarshalPersona**

Add to `parse_test.go`:

```go
func TestMarshalRoundtrip(t *testing.T) {
	original := &Persona{
		ID: "test", Name: "Test", Icon: "T",
		Color: "#000", Prompt: "Do the thing.",
		Expectations: "Report format.", BuiltIn: false,
	}
	data, err := MarshalPersona(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	parsed, err := ParsePersona(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if parsed.ID != original.ID || parsed.Name != original.Name || parsed.Prompt != original.Prompt {
		t.Errorf("roundtrip mismatch: got %+v", parsed)
	}
}
```

**Step 6: Run tests**

```bash
go test ./internal/persona/...
```

Expected: PASS

**Step 7: Commit**

```
/commit
```

Message: `feat(persona): add YAML frontmatter parser for persona files`

---

### Task 2: Persona Manager (CRUD)

**Files:**

- Create: `internal/persona/manager.go`
- Test: `internal/persona/manager_test.go`

**Step 1: Write failing tests**

Create `internal/persona/manager_test.go`:

```go
package persona

import (
	"testing"
)

func TestManagerList(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	personas, err := mgr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(personas) != 0 {
		t.Errorf("expected empty list, got %d", len(personas))
	}
}

func TestManagerCreateAndGet(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{
		ID: "test-persona", Name: "Test", Icon: "T",
		Color: "#fff", Prompt: "Be helpful.", BuiltIn: false,
	}
	if err := mgr.Create(p); err != nil {
		t.Fatalf("create error: %v", err)
	}

	got, err := mgr.Get("test-persona")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if got.Name != "Test" || got.Prompt != "Be helpful." {
		t.Errorf("unexpected persona: %+v", got)
	}
}

func TestManagerCreateDuplicate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{ID: "dup", Name: "Dup", Icon: "D", Color: "#000"}
	if err := mgr.Create(p); err != nil {
		t.Fatalf("first create error: %v", err)
	}
	if err := mgr.Create(p); err == nil {
		t.Fatal("expected error on duplicate create")
	}
}

func TestManagerUpdate(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	p := &Persona{ID: "upd", Name: "Original", Icon: "O", Color: "#000"}
	mgr.Create(p)

	p.Name = "Updated"
	if err := mgr.Update(p); err != nil {
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

	p := &Persona{ID: "del", Name: "Del", Icon: "X", Color: "#000"}
	mgr.Create(p)

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
		t.Fatal("expected error for missing persona")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/persona/...
```

Expected: FAIL — `NewManager` not defined.

**Step 3: Write minimal implementation**

Create `internal/persona/manager.go`:

```go
package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manager handles CRUD operations on persona YAML files.
type Manager struct {
	dir string
}

// NewManager creates a Manager that stores personas in the given directory.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// Dir returns the personas directory path.
func (m *Manager) Dir() string {
	return m.dir
}

// List returns all personas sorted by name.
func (m *Manager) List() ([]*Persona, error) {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create personas directory: %w", err)
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read personas directory: %w", err)
	}

	var personas []*Persona
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		p, err := ParsePersona(data)
		if err != nil {
			continue
		}
		personas = append(personas, p)
	}

	sort.Slice(personas, func(i, j int) bool {
		return personas[i].Name < personas[j].Name
	})
	return personas, nil
}

// Get returns a persona by ID.
func (m *Manager) Get(id string) (*Persona, error) {
	path := filepath.Join(m.dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("persona not found: %s", id)
		}
		return nil, fmt.Errorf("failed to read persona %s: %w", id, err)
	}
	return ParsePersona(data)
}

// Create writes a new persona file. Fails if the ID already exists.
func (m *Manager) Create(p *Persona) error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
	}

	path := filepath.Join(m.dir, p.ID+".yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("persona already exists: %s", p.ID)
	}

	data, err := MarshalPersona(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Update overwrites an existing persona file. Fails if the ID doesn't exist.
func (m *Manager) Update(p *Persona) error {
	path := filepath.Join(m.dir, p.ID+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("persona not found: %s", p.ID)
	}

	data, err := MarshalPersona(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Delete removes a persona file.
func (m *Manager) Delete(id string) error {
	path := filepath.Join(m.dir, id+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("persona not found: %s", id)
	}
	return os.Remove(path)
}
```

**Step 4: Run tests**

```bash
go test ./internal/persona/...
```

Expected: PASS

**Step 5: Commit**

```
/commit
```

Message: `feat(persona): add persona manager with filesystem CRUD`

---

### Task 3: Built-in Personas

**Files:**

- Create: `internal/persona/builtins/security-auditor.yaml` (and 4 more)
- Create: `internal/persona/builtins.go`
- Modify: `internal/persona/manager.go` — add `EnsureBuiltins()`
- Test: `internal/persona/manager_test.go` — add builtin tests

**Step 1: Create the 5 built-in persona YAML files**

Create `internal/persona/builtins/` directory and 5 YAML files. The content for each persona should follow the format in `docs/specs/personas.md`. Write thorough, well-crafted system prompts for each — these are the product's first impression. The expectations field should clearly distinguish report-oriented personas (Technical PM) from code-change personas (Security Auditor, QA Engineer) from hybrid personas (Docs Writer).

**Step 2: Create the embed file**

Create `internal/persona/builtins.go`:

```go
package persona

import "embed"

//go:embed builtins/*.yaml
var builtinsFS embed.FS
```

**Step 3: Write failing test for EnsureBuiltins**

Add to `manager_test.go`:

```go
func TestEnsureBuiltins(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	if err := mgr.EnsureBuiltins(); err != nil {
		t.Fatalf("ensure error: %v", err)
	}

	personas, err := mgr.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(personas) != 5 {
		t.Errorf("expected 5 built-in personas, got %d", len(personas))
	}

	// Verify all are marked built_in
	for _, p := range personas {
		if !p.BuiltIn {
			t.Errorf("persona %q should be built_in", p.ID)
		}
	}
}

func TestEnsureBuiltinsSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// First ensure
	mgr.EnsureBuiltins()

	// Modify one
	p, _ := mgr.Get("security-auditor")
	p.Prompt = "Custom prompt."
	mgr.Update(p)

	// Second ensure should not overwrite
	mgr.EnsureBuiltins()

	got, _ := mgr.Get("security-auditor")
	if got.Prompt != "Custom prompt." {
		t.Error("EnsureBuiltins overwrote user-modified persona")
	}
}
```

**Step 4: Run tests to verify they fail**

```bash
go test ./internal/persona/...
```

Expected: FAIL — `EnsureBuiltins` not defined.

**Step 5: Implement EnsureBuiltins**

Add to `manager.go`:

```go
// EnsureBuiltins writes built-in personas to disk if they don't already exist.
// Does not overwrite user-modified personas.
func (m *Manager) EnsureBuiltins() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
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

		// Skip if file already exists (don't overwrite user edits)
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
```

**Step 6: Run tests**

```bash
go test ./internal/persona/...
```

Expected: PASS

**Step 7: Add a `ResetBuiltIn` method for the "reset to default" UI action**

Add to manager and test: `ResetBuiltIn(id string)` deletes the file and re-copies from embedded. Test that it restores original content.

**Step 8: Commit**

```
/commit
```

Message: `feat(persona): add built-in personas with embedded YAML files`

---

### Task 4: API Contracts and Type Generation

**Files:**

- Create: `internal/api/contracts/persona.go`
- Modify: `cmd/gen-types/main.go:27-36` — add persona root type
- Modify: `internal/state/state.go:113` — add `PersonaID` to Session
- Regenerate: `assets/dashboard/src/lib/types.generated.ts`

**Step 1: Create contract types**

Create `internal/api/contracts/persona.go`:

```go
package contracts

// Persona represents a behavioral profile for an agent.
type Persona struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Color        string `json:"color"`
	Prompt       string `json:"prompt"`
	Expectations string `json:"expectations"`
	BuiltIn      bool   `json:"built_in"`
}

// PersonaCreateRequest is the body for POST /api/personas.
type PersonaCreateRequest struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Color        string `json:"color"`
	Prompt       string `json:"prompt"`
	Expectations string `json:"expectations,omitempty"`
}

// PersonaUpdateRequest is the body for PUT /api/personas/{id}.
type PersonaUpdateRequest struct {
	Name         *string `json:"name,omitempty"`
	Icon         *string `json:"icon,omitempty"`
	Color        *string `json:"color,omitempty"`
	Prompt       *string `json:"prompt,omitempty"`
	Expectations *string `json:"expectations,omitempty"`
}

// PersonaListResponse is the body for GET /api/personas.
type PersonaListResponse struct {
	Personas []Persona `json:"personas"`
}
```

**Step 2: Add PersonaID to Session state**

In `internal/state/state.go`, add to the Session struct (after `Nickname` field, line ~117):

```go
PersonaID string `json:"persona_id,omitempty"`
```

**Step 3: Add root type to gen-types**

In `cmd/gen-types/main.go`, add to the `rootTypes` slice (line ~36):

```go
reflect.TypeOf(contracts.PersonaListResponse{}),
reflect.TypeOf(contracts.PersonaCreateRequest{}),
reflect.TypeOf(contracts.PersonaUpdateRequest{}),
```

**Step 4: Regenerate TypeScript types**

```bash
go run ./cmd/gen-types
```

Verify `assets/dashboard/src/lib/types.generated.ts` now contains `Persona`, `PersonaListResponse`, `PersonaCreateRequest`, and `PersonaUpdateRequest` interfaces.

**Step 5: Commit**

```
/commit
```

Message: `feat(persona): add API contracts, session persona tracking, and generated types`

---

### Task 5: API Handlers

**Files:**

- Create: `internal/dashboard/handlers_personas.go`
- Modify: `internal/dashboard/server.go:533` — add persona routes
- Modify: `internal/dashboard/server.go` — add persona manager field to Server struct and initialization

**Step 1: Wire the persona manager into the Server**

The `Server` struct in `internal/dashboard/server.go` needs a `*persona.Manager` field. Initialize it in the constructor, pointing at `~/.schmux/personas/`. Call `EnsureBuiltins()` during server startup.

Find where the Server is created and where `~/.schmux/` path is resolved (likely from `config.GetSchmuxDir()` or similar). The personas dir is `filepath.Join(schmuxDir, "personas")`.

**Step 2: Create handler file**

Create `internal/dashboard/handlers_personas.go` following the exact pattern from `handlers_remote.go`:

```go
package dashboard

// Handlers:
// handleListPersonas    GET    /api/personas
// handleGetPersona      GET    /api/personas/{id}
// handleCreatePersona   POST   /api/personas
// handleUpdatePersona   PUT    /api/personas/{id}
// handleDeletePersona   DELETE /api/personas/{id}
```

Each handler should:

- List: call `mgr.List()`, map to contract types, return JSON
- Get: extract `id` from `chi.URLParam(r, "id")`, call `mgr.Get(id)`
- Create: decode `PersonaCreateRequest`, validate required fields (id, name, icon, color, prompt), create `Persona`, call `mgr.Create()`
- Update: get existing, apply non-nil fields from `PersonaUpdateRequest`, call `mgr.Update()`
- Delete: if persona is built-in, call `mgr.ResetBuiltIn(id)`; otherwise call `mgr.Delete(id)`

Validate the `id` field: must be URL-safe slug (lowercase alphanumeric + hyphens). Reject IDs with path separators or special characters (prevent path traversal).

**Step 3: Register routes**

In `server.go`, in the CSRF-protected API group (near line 533 where remote flavor routes are), add:

```go
r.Get("/personas", s.handleListPersonas)
r.Post("/personas", s.handleCreatePersona)
r.Get("/personas/{id}", s.handleGetPersona)
r.Put("/personas/{id}", s.handleUpdatePersona)
r.Delete("/personas/{id}", s.handleDeletePersona)
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/... ./internal/persona/...
```

Expected: PASS

**Step 5: Manual smoke test**

```bash
go build ./cmd/schmux && ./schmux daemon-run
# In another terminal:
curl http://localhost:7337/api/personas | jq .
```

Verify the 5 built-in personas are returned.

**Step 6: Commit**

```
/commit
```

Message: `feat(persona): add CRUD API endpoints for personas`

---

### Task 6: Spawn Integration (Backend)

**Files:**

- Modify: `internal/session/manager.go:570` — add `PersonaID` to `SpawnOptions`
- Modify: `internal/session/manager.go:589` — inject persona prompt in `Spawn()`
- Modify: `internal/session/manager.go:953` — add `appendPersonaFlags()` or extend `appendSignalingFlags()`
- Modify: `internal/workspace/ensure/manager.go:159` — add persona injection for instruction-file agents
- Modify: `internal/dashboard/handlers_spawn.go:28` — add `PersonaID` to `SpawnRequest`
- Modify: `internal/dashboard/handlers_spawn.go:270` — pass `PersonaID` through to `SpawnOptions`

**Step 1: Add PersonaID to SpawnOptions**

In `internal/session/manager.go`, add to `SpawnOptions` (line ~580):

```go
PersonaID string
```

**Step 2: Add PersonaID to SpawnRequest**

In `internal/dashboard/handlers_spawn.go`, add to `SpawnRequest` (line ~39):

```go
PersonaID string `json:"persona_id,omitempty"`
```

Pass it through in both spawn call sites (line ~271 for local, line ~267 for remote).

**Step 3: Inject persona in the spawn flow**

The persona manager needs to be accessible from the session manager. Either pass it as a dependency or pass the resolved persona prompt through `SpawnOptions`.

In `Spawn()` (line ~589), after workspace provisioning and before building the command:

1. If `opts.PersonaID != ""`, load the persona via the manager
2. Write the persona prompt (with expectations prepended) to a file at `<workspace>/.schmux/persona-<sessionID>.md`
3. For Claude (hooks-based): pass the file via `--append-system-prompt-file`
4. For Claude (non-hooks): add alongside existing signaling file
5. For Codex/Gemini: inject into agent instruction files using the same marker pattern as signaling, but with `<!-- SCHMUX:PERSONA:BEGIN -->` / `<!-- SCHMUX:PERSONA:END -->` markers

The persona file content should combine expectations and prompt:

```
## Persona: {name}

### Behavioral Expectations
{expectations}

### Instructions
{prompt}
```

**Step 4: Store PersonaID on session state**

In `Spawn()`, when creating the `state.Session` struct (find where `sess = &state.Session{...}` is built), set `PersonaID: opts.PersonaID`.

**Step 5: Run tests**

```bash
go test ./internal/session/... ./internal/dashboard/...
```

Expected: PASS

**Step 6: Commit**

```
/commit
```

Message: `feat(persona): inject persona prompt at spawn time via agent-native mechanisms`

---

### Task 7: Dashboard — Persona Management Page

**Files:**

- Create: `assets/dashboard/src/routes/PersonasPage.tsx`
- Create: `assets/dashboard/src/routes/PersonasPage.css`
- Modify: `assets/dashboard/src/lib/api.ts` — add persona API functions
- Modify: `assets/dashboard/src/App.tsx` (or wherever routes are defined) — add `/personas` route
- Modify: `assets/dashboard/src/components/AppShell.tsx:860` — add nav link above overlays
- Test: `assets/dashboard/src/routes/PersonasPage.test.tsx`

**Step 1: Add API functions**

Add to `assets/dashboard/src/lib/api.ts`:

```typescript
export async function getPersonas(): Promise<PersonaListResponse> { ... }
export async function createPersona(req: PersonaCreateRequest): Promise<Persona> { ... }
export async function updatePersona(id: string, req: PersonaUpdateRequest): Promise<Persona> { ... }
export async function deletePersona(id: string): Promise<void> { ... }
```

Follow the existing fetch pattern in the file (check how `getConfig`, remote flavor functions, etc. are implemented).

**Step 2: Write the failing test**

Create `assets/dashboard/src/routes/PersonasPage.test.tsx`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// Mock the API and contexts following the pattern in SpawnPage tests
// Test that:
// - Page renders a list of persona cards
// - Each card shows icon, name, and prompt preview
// - Built-in personas show a "Built-in" badge
// - Create button opens a form
// - Delete on built-in shows "Reset to default"
```

**Step 3: Implement PersonasPage**

Create `assets/dashboard/src/routes/PersonasPage.tsx`:

- Grid layout of persona cards
- Each card: icon (large), name, color accent bar left edge, prompt preview (first ~2 lines), "Built-in" badge
- Edit/Delete actions per card
- "Create Persona" button opens inline form or modal
- Form fields: name, id (auto-slugified from name), icon (text input for emoji), color (color input), expectations (textarea), prompt (large textarea)
- On save: call `createPersona()` or `updatePersona()`

Follow existing page patterns in the project for styling, layout, and data fetching.

**Step 4: Add route**

In the routes configuration (find where `SpawnPage`, `OverlaysPage`, etc. are registered), add:

```typescript
{ path: '/personas', element: <PersonasPage /> }
```

**Step 5: Add nav link**

In `AppShell.tsx`, **above** the Overlays nav link (line ~861), add a "Personas" NavLink. Use an appropriate SVG icon (a user/mask icon works well). The nav link should always be visible (not conditional on `config?.repos?.length`).

**Step 6: Run frontend tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 7: Commit**

```
/commit
```

Message: `feat(persona): add persona management page with CRUD UI`

---

### Task 8: Dashboard — Spawn Wizard Integration

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx` — add persona selector
- Modify: `assets/dashboard/src/lib/types.ts:111` — add `persona_id` to `SpawnRequest`
- Test: `assets/dashboard/src/routes/SpawnPage.test.tsx` or new test file

**Step 1: Add persona_id to the frontend SpawnRequest type**

In `assets/dashboard/src/lib/types.ts`, add to the `SpawnRequest` interface:

```typescript
persona_id?: string;
```

**Step 2: Fetch personas in SpawnPage**

Add a `useEffect` to fetch personas via `getPersonas()`. Store in state.

**Step 3: Add persona selector — single mode**

Below the agent/model dropdown (after the agent selector section), add a persona dropdown:

```tsx
<select value={selectedPersonaId} onChange={...}>
  <option value="">No persona</option>
  {personas.map(p => (
    <option key={p.id} value={p.id}>{p.icon} {p.name}</option>
  ))}
</select>
```

Include a small link: "Manage personas" that navigates to `/personas`.

**Step 4: Add persona selector — advanced mode**

In advanced mode (where each agent row has +/- controls), add a persona dropdown per agent row. This requires changing the state model from `Record<string, number>` (target counts) to something that also tracks persona per target. Consider a structure like `Record<string, { count: number; personaId?: string }>` or a parallel `Record<string, string>` for persona assignments.

**Step 5: Wire persona_id into the spawn request**

When building the `SpawnRequest` to send to the API, include the selected `persona_id`.

**Step 6: Write tests**

Test that:

- Persona dropdown appears in single mode
- Persona dropdown appears per-agent in advanced mode
- Selecting a persona includes `persona_id` in the spawn request
- "No persona" sends no `persona_id`

**Step 7: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 8: Commit**

```
/commit
```

Message: `feat(persona): add persona selector to spawn wizard`

---

### Task 9: Dashboard — Session Display

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx:821-841` — add persona badge to session rows
- Modify: `assets/dashboard/src/lib/types.ts` — add `persona_id` and `persona_icon`/`persona_color` to session types (or fetch persona data separately)

**Step 1: Surface persona info in session data**

The WebSocket dashboard broadcast sends session state to the frontend. Check how session data reaches the frontend (look at `SessionsContext` or the WebSocket handler). The session already has `persona_id` from Task 4.

The frontend needs the persona's icon and color for display. Two options:

- **Option A:** Have the backend include `persona_icon` and `persona_color` in the session broadcast (denormalized). Simpler for the frontend.
- **Option B:** Frontend looks up persona by ID from its own cached persona list.

Option A is recommended — add `PersonaIcon` and `PersonaColor` fields to the session broadcast response (not persisted to state, computed at broadcast time by looking up the persona).

**Step 2: Add persona badge to session rows**

In `AppShell.tsx`, in the session row (line ~841, where `{sess.nickname || sess.target}` is rendered), add the persona icon as a badge:

```tsx
{
  sess.persona_icon && (
    <span
      className="nav-session__persona-badge"
      title={sess.persona_name}
      style={{ color: sess.persona_color }}
    >
      {sess.persona_icon}
    </span>
  );
}
```

Add CSS for `.nav-session__persona-badge` — small, positioned to the right of the session name, with the persona's color.

**Step 3: Add persona accent to session detail page**

Find the session detail view (likely in `SessionPage.tsx` or similar). Add the persona name and a color accent bar.

**Step 4: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 5: Commit**

```
/commit
```

Message: `feat(persona): display persona icon badge on session cards`

---

### Task 10: Update API Documentation

**Files:**

- Modify: `docs/api.md` — add persona endpoints

**Step 1: Document the persona API**

Add a "Personas" section to `docs/api.md` documenting:

- `GET /api/personas` — list all personas
- `GET /api/personas/{id}` — get single persona
- `POST /api/personas` — create persona (request body fields)
- `PUT /api/personas/{id}` — update persona (partial update fields)
- `DELETE /api/personas/{id}` — delete or reset built-in

Document the `persona_id` addition to `SpawnRequest` and session state.

Follow the existing documentation style in the file.

**Step 2: Commit**

```
/commit
```

Message: `docs: add persona API endpoints to api.md`

---

### Task 11: End-to-End Verification

**Step 1: Build everything**

```bash
go run ./cmd/build-dashboard && go build ./cmd/schmux
```

**Step 2: Run all tests**

```bash
./test.sh --all
```

Expected: PASS

**Step 3: Manual verification**

1. Start daemon: `./schmux daemon-run`
2. Open dashboard at `http://localhost:7337`
3. Navigate to `/personas` — verify 5 built-in personas appear
4. Create a custom persona — verify it appears in the grid
5. Edit a persona — verify changes persist
6. Delete a custom persona — verify it's removed
7. Delete a built-in persona — verify it resets to default
8. Go to spawn wizard — verify persona dropdown appears
9. Spawn an agent with a persona — verify the session card shows the persona icon
10. Check session detail — verify persona name and color accent
