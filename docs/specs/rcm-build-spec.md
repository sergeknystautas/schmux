# Repository Construction Model (RCM) - Build Specification

## Overview

This spec defines the implementation contract for the RCM analysis system. If it's not in this spec, don't build it.

**Deliverable**: An API endpoint that analyzes a repository and produces a machine-readable JSON artifact for LLM consumption.

---

## API Contract

### Endpoint

```
POST /api/precog/repo/{repoName}
```

**Request**: Empty body. The `repoName` path parameter identifies the repo (matches `config.Repos[].Name`).

**Response** (immediate):

```json
{
  "status": "started",
  "job_id": "rcm-schmux-1707984000"
}
```

Analysis runs async. The endpoint returns immediately.

### Status Check

```
GET /api/precog/repo/{repoName}/status
```

**Response**:

```json
{
  "status": "running|completed|failed",
  "current_pass": "B",
  "started_at": "2024-02-15T10:00:00Z",
  "completed_at": null,
  "error": null
}
```

### Get Result

```
GET /api/precog/repo/{repoName}
```

**Response**: The full RCM JSON (see Output Schema below), or 404 if no analysis exists.

---

## Storage

```
~/.schmux/precog/{repoName}.json      # The RCM output
~/.schmux/precog/{repoName}.meta.json # Job metadata
```

**Meta schema**:

```json
{
  "job_id": "rcm-schmux-1707984000",
  "status": "completed",
  "started_at": "2024-02-15T10:00:00Z",
  "completed_at": "2024-02-15T10:05:00Z",
  "commit_hash": "a70d034b",
  "error": null
}
```

---

## Output Schema (RCM JSON)

```json
{
  "repo_summary": {
    "name": "schmux",
    "analyzed_at": "2024-02-15T10:05:00Z",
    "commit_hash": "a70d034b",
    "system_type": "daemon with web dashboard",
    "primary_languages": ["go", "typescript"],
    "lines_of_code": 45000
  },
  "runtime_components": [],
  "entrypoints": [],
  "capabilities": [],
  "contracts": [],
  "clusters": [],
  "couplings": [],
  "drift_findings": [],
  "gravity": [],
  "trajectory": [],
  "confidence": {}
}
```

Each section is defined below with its exact schema.

---

## Pass A: Inventory & Entry Points

### Goal

Determine what kind of system this is and where execution begins.

### Static Analysis

1. List all files: `git -C <bare> ls-tree -r --name-only HEAD`
2. Detect languages by extension
3. Find entry point patterns:
   - Go: `func main()` in `cmd/` or root
   - HTTP handlers: route registration patterns
   - CLI: cobra/urfave command definitions
4. Count lines per language

### LLM Prompt

```
You are analyzing a software repository to understand its structure.

Here is the file tree:
{file_tree}

Here are the detected entry points:
{entry_points}

Here is the content of key files:
{main_files_content}

Classify this system:
1. What type of system is this? (daemon, CLI tool, web app, library, etc.)
2. What are the runtime components? (services, workers, UIs, databases)
3. What are the entry points for each component?

Respond in JSON:
{
  "system_type": "string - brief description",
  "runtime_components": [
    {"name": "string", "type": "service|worker|ui|library|cli", "anchors": ["file:line"]}
  ],
  "entrypoints": [
    {"type": "api|worker|ui|event|cli", "anchor": "file:line", "notes": "string"}
  ]
}
```

### Output Schema

```json
"runtime_components": [
  {
    "name": "dashboard-server",
    "type": "service",
    "anchors": ["internal/dashboard/server.go:45"]
  }
],
"entrypoints": [
  {
    "type": "cli",
    "anchor": "cmd/schmux/main.go:15",
    "notes": "Main CLI entry, delegates to daemon"
  },
  {
    "type": "api",
    "anchor": "internal/dashboard/handlers.go:100",
    "notes": "HTTP API routes"
  }
]
```

---

## Pass B: Capability Mining

### Goal

Infer what the system does, not how it's organized. Produce 8-20 capabilities.

### Static Analysis

1. Extract package names and their exports
2. Find route patterns (HTTP, RPC)
3. Extract database table/collection names from schemas or migrations
4. Extract type names from public interfaces
5. Parse test file names for domain hints

### LLM Prompt

```
You are identifying the capabilities of a software system.

Here are the packages and their public symbols:
{packages_and_symbols}

Here are the API routes:
{routes}

Here are the database schemas/tables:
{schemas}

Here are domain types:
{types}

Identify 8-20 capabilities. Each capability is a coherent domain function the system provides.

Avoid generic buckets like "utils", "common", "shared", "misc".

Respond in JSON:
{
  "capabilities": [
    {
      "id": "cap-session-management",
      "name": "Session Management",
      "description": "Manages tmux sessions for running AI agents",
      "keywords": ["session", "tmux", "spawn", "dispose"],
      "anchors": {
        "entrypoints": ["internal/dashboard/handlers.go:200"],
        "modules": ["internal/session/"],
        "schema": [],
        "symbols": ["SessionManager", "SpawnSession", "DisposeSession"]
      }
    }
  ]
}
```

### Output Schema

```json
"capabilities": [
  {
    "id": "cap-{slug}",
    "name": "Human Readable Name",
    "description": "What this capability does",
    "keywords": ["keyword1", "keyword2"],
    "anchors": {
      "entrypoints": ["file:line"],
      "modules": ["path/to/module/"],
      "schema": ["table_name or schema file"],
      "symbols": ["SymbolName"]
    }
  }
]
```

---

## Pass C: Coordination Surfaces (Contracts)

### Goal

Find structures that force sequencing and collaboration.

### Static Analysis

1. Compute import fan-in (which files are imported most)
2. Find shared types used across packages
3. Identify API contracts (OpenAPI, protobuf, GraphQL schemas)
4. Find config keys and where they're read
5. Find event/message definitions

### LLM Prompt

```
You are identifying coordination surfaces in a codebase - the contracts that force teams to coordinate.

Here are the most-imported files (high fan-in):
{high_fanin_files}

Here are shared types used across multiple packages:
{shared_types}

Here are the capabilities identified:
{capabilities}

For each coordination surface:
1. What type is it? (api_schema, shared_model, db_schema, event, config, auth_policy, library)
2. Which capabilities use it?
3. What is its coordination pressure? (how many things depend on it)

Respond in JSON:
{
  "contracts": [
    {
      "id": "contract-{slug}",
      "type": "api_schema|shared_model|db_schema|event|config|auth_policy|library",
      "name": "Human Name",
      "anchor": "file:line or file path",
      "used_by_capabilities": ["cap-id-1", "cap-id-2"],
      "fan_in": 15,
      "notes": "Why this matters for coordination"
    }
  ]
}
```

### Output Schema

```json
"contracts": [
  {
    "id": "contract-{slug}",
    "type": "shared_model",
    "name": "Session State",
    "anchor": "internal/state/state.go:19",
    "used_by_capabilities": ["cap-session-management", "cap-dashboard"],
    "fan_in": 12,
    "notes": "Core state shared across session and dashboard capabilities"
  }
]
```

---

## Pass D: Reality Map (Clusters & Couplings)

### Goal

Model how the system actually behaves vs. how it's organized.

### Static Analysis

**Structural Graph**:

1. Build import graph between packages
2. Build symbol reference graph (which functions call which)
3. Identify clusters of tightly coupled files

**Evolution Graph** (from git history):

1. Get co-change data: files that frequently change together
   ```
   git -C <bare> log --name-only --pretty=format:"COMMIT:%H" -n 500
   ```
2. Build co-change frequency matrix
3. Cluster files by co-change patterns

### LLM Prompt

```
You are analyzing the actual coupling in a codebase.

Here are file clusters based on import relationships:
{structural_clusters}

Here are file clusters based on co-change history:
{evolution_clusters}

Here are the capabilities:
{capabilities}

Identify:
1. Clusters that span multiple capabilities (coordination knots)
2. Hidden couplings not obvious from folder structure
3. Cyclic dependencies

Respond in JSON:
{
  "clusters": [
    {
      "id": "cluster-{slug}",
      "type": "structural|evolutionary|hybrid",
      "name": "Human description",
      "members": ["file1.go", "file2.go"],
      "capabilities_involved": ["cap-id-1", "cap-id-2"]
    }
  ],
  "couplings": [
    {
      "capability_a": "cap-id-1",
      "capability_b": "cap-id-2",
      "strength": "high|medium|low",
      "evidence": ["Both depend on state.Session", "Co-change 80% of commits"]
    }
  ]
}
```

### Output Schema

```json
"clusters": [
  {
    "id": "cluster-{slug}",
    "type": "hybrid",
    "name": "Session-Terminal coupling",
    "members": ["internal/session/manager.go", "internal/dashboard/websocket.go"],
    "capabilities_involved": ["cap-session-management", "cap-terminal-streaming"]
  }
],
"couplings": [
  {
    "capability_a": "cap-session-management",
    "capability_b": "cap-terminal-streaming",
    "strength": "high",
    "evidence": [
      "Shared Session type",
      "Co-change in 75% of session-related commits"
    ]
  }
]
```

---

## Pass E: Architectural Drift

### Goal

Detect mismatches between intended structure and emergent behavior.

### Static Analysis

1. Extract declared boundaries from:
   - Folder structure (each top-level folder = intended boundary)
   - Architecture docs (if present)
   - Package comments
2. Compare against observed couplings from Pass D

### LLM Prompt

```
You are detecting architectural drift - where reality diverges from intent.

Declared structure (from folders and docs):
{declared_structure}

Observed couplings (from Pass D):
{observed_couplings}

Cross-boundary dependencies:
{cross_boundary_deps}

For each drift finding:
1. What was the declared boundary?
2. What does observed behavior show?
3. How does this impact parallel development?

Respond in JSON:
{
  "drift_findings": [
    {
      "id": "drift-{slug}",
      "declared_boundary": "dashboard should not directly access tmux",
      "observed_behavior": "handlers.go imports internal/tmux",
      "impact_on_parallel_work": "Changes to tmux may unexpectedly break dashboard",
      "anchors": ["internal/dashboard/handlers.go:15"]
    }
  ]
}
```

### Output Schema

```json
"drift_findings": [
  {
    "id": "drift-{slug}",
    "declared_boundary": "Description of intended boundary",
    "observed_behavior": "What actually happens",
    "impact_on_parallel_work": "Why this matters for coordination",
    "anchors": ["file:line"]
  }
]
```

---

## Pass F: Change Gravity & Trajectory

### Goal

Predict where future work will land.

### Static Analysis

1. Compute churn by capability (commits touching each capability)
   ```
   git -C <bare> log --name-only --since="3 months ago" --pretty=format:""
   ```
2. Identify recently added files/endpoints/types
3. Detect expanding vs stabilizing regions

### LLM Prompt

```
You are predicting where future development effort will concentrate.

Churn by capability (commits in last 3 months):
{capability_churn}

Churn by contract:
{contract_churn}

Recently added code (new files, endpoints, types):
{recent_additions}

For each gravity zone:
1. What region is attracting work?
2. What signals indicate this?
3. What does this imply for coordination?

For trajectory:
1. What direction is the system evolving?
2. Where will parallel work likely collide?

Respond in JSON:
{
  "gravity": [
    {
      "region": "cap-remote-connections",
      "type": "capability",
      "signals": ["45 commits in 3 months", "3 new endpoints added"],
      "implication": "Active development - high collision risk"
    }
  ],
  "trajectory": [
    {
      "direction": "Expanding remote execution capabilities",
      "evidence": ["New remote_flavors config", "SSH tunnel handlers"],
      "confidence": "high"
    }
  ]
}
```

### Output Schema

```json
"gravity": [
  {
    "region": "capability or contract id",
    "type": "capability|contract",
    "signals": ["signal 1", "signal 2"],
    "implication": "What this means"
  }
],
"trajectory": [
  {
    "direction": "Description of evolution direction",
    "evidence": ["evidence 1", "evidence 2"],
    "confidence": "high|medium|low"
  }
]
```

---

## Confidence Section

Every RCM must include confidence estimates:

```json
"confidence": {
  "capabilities": "high|medium|low",
  "capabilities_notes": "Clear package boundaries made capability extraction reliable",
  "contracts": "medium",
  "contracts_notes": "Some internal types may have been missed",
  "clusters": "medium",
  "clusters_notes": "Limited git history available",
  "drift": "low",
  "drift_notes": "No architecture docs found to compare against",
  "trajectory": "medium",
  "trajectory_notes": "3 months of history analyzed"
}
```

---

## Config Addition

Add to `internal/config/config.go` (minimal additions only):

```go
type PrecogConfig struct {
    Target  string `json:"target,omitempty"`  // run target for LLM calls
    Timeout int    `json:"timeout,omitempty"` // seconds per pass, default 120
}
```

Add field to `Config` struct:

```go
Precog *PrecogConfig `json:"precog,omitempty"`
```

Add getter (follow existing pattern like `GetLoreTarget`):

```go
func (c *Config) GetPrecogTarget() string {
    if c.Precog != nil && c.Precog.Target != "" {
        return c.Precog.Target
    }
    return c.GetCompoundTarget() // fallback chain
}

func (c *Config) GetPrecogTimeout() int {
    if c.Precog != nil && c.Precog.Timeout > 0 {
        return c.Precog.Timeout
    }
    return 120 // default 2 minutes per pass
}
```

Note: Config struct fields must be in `config.go`, but keep additions minimal.

---

## Implementation Files

```
internal/precog/
  rcm.go              # Orchestrator: runs passes, assembles output
  passes.go           # Pass definitions (prompts, response parsing)
  static.go           # Static analysis helpers (reuse existing git helpers)
  static_test.go      # Tests for static analysis

internal/dashboard/
  handlers_precog.go  # API endpoints (NEW FILE - do not add to handlers.go)

internal/api/contracts/
  precog.go           # Request/response types (NEW FILE)

~/.schmux/precog/     # Output directory (created on first use)
```

### Code Reuse Requirements

**Do NOT duplicate git operations.** Reuse existing helpers:

- `lore.ReadFileFromRepo(ctx, bareDir, path)` - read file from bare repo HEAD
- `workspace.GetQueryBasePath()` pattern for bare repo paths
- `config.FindRepoByName()` / `config.FindRepoByURL()` for repo lookup
- `oneshot.ExecuteTarget()` for LLM calls

**Create new files, not patches to large existing files:**

- `handlers_precog.go` not edits to `handlers.go`
- `contracts/precog.go` not edits to existing contract files
- `config/precog.go` for config helpers if needed (vs editing `config.go` directly)

---

## Bare Repo Access Pattern

All git operations run against the bare clone at `~/.schmux/query/{repo.BarePath}`.

### Reuse Existing Helpers

```go
// Get bare repo path - use existing pattern from workspace/lore packages
bareDir := filepath.Join(cfg.GetQueryBasePath(), repo.BarePath)

// Read file from HEAD - use existing lore helper
content, err := lore.ReadFileFromRepo(ctx, bareDir, "path/to/file.go")
```

### New Git Queries (add to internal/precog/static.go)

Only add new git helpers if no existing helper covers the need:

```go
// List all files in repo
func listFiles(ctx context.Context, bareDir string) ([]string, error)

// Get commit log with changed files
func getCommitLog(ctx context.Context, bareDir string, limit int) ([]CommitInfo, error)

// Get file content at specific commit
func getFileAtCommit(ctx context.Context, bareDir, commit, path string) (string, error)
```

These should follow the same patterns as `internal/workspace/origin_queries.go` and `internal/lore/curator.go`.

---

## LLM Execution Pattern

Use existing `oneshot.ExecuteTarget`:

```go
func (r *RCMAnalyzer) runLLMPass(ctx context.Context, passName, prompt string) (string, error) {
    target := r.config.GetPrecogTarget()
    timeout := time.Duration(r.config.GetPrecogTimeout()) * time.Second

    return oneshot.ExecuteTarget(ctx, r.config, target, prompt,
        schema.LabelPrecog, timeout, r.bareDir)
}
```

Add schema label in `internal/oneshot/schema/`:

```go
const LabelPrecog = "precog"
```

---

## Error Handling

- If a pass fails, log the error and continue with reduced confidence
- If static analysis fails (git errors), abort the whole job
- Store partial results if possible (some passes may complete)

---

## Naming Convention

Use "precog" consistently across:

- Package name: `internal/precog/`
- API paths: `/api/precog/...`
- Config block: `precog`
- Storage directory: `~/.schmux/precog/`
- Schema label: `schema.LabelPrecog`
- Any future CLI commands: `schmux precog ...`

---

## Not In Scope

- Real-time updates / WebSocket streaming of progress
- Incremental analysis (always full re-analysis)
- Multiple concurrent analyses of the same repo
- User-facing visualization
- Workspace-level analysis (this is repo-level only)

---

## Acceptance Criteria

The RCM JSON must enable an LLM to answer:

1. What are the real capabilities of this system?
2. Where must coordination occur when making changes?
3. Where does reality contradict the folder structure?
4. Where is development effort concentrating?
5. If I change capability X, what else might be affected?

If the JSON cannot support these queries, the implementation is incomplete.
