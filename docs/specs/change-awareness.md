# Intent-Aware Change Awareness System

## Executive Summary

This document summarizes the strategic goals and architectural approach
for building an automated, LLM-native system that provides proactive
awareness of parallel development work across agents and
developers---without requiring enforced structure such as formal
specification files.

The system's purpose is **not** alerting or visualization for its own
sake. Instead, it externalizes cognitive load, enabling developers to
manage multiple concurrent agent-driven workstreams while preventing
design divergence and sequencing mistakes before they materialize in
code.

---

# Core Goals

## 1. Externalize Developer Cognitive Load

Developers operating multiple agents must currently track:

- What each agent is building
- Which changes must land first
- Where overlaps may occur
- Who has historical context in affected areas

The system should automatically construct this understanding so it no
longer lives in the developer's head.

---

## 2. Provide Continuous Situational Awareness (Not Warnings)

The objective is awareness, not interruption.

Outputs should help developers answer:

- What is currently being built?
- How are these efforts related?
- What should land first?
- Where should collaboration happen early?
- Which streams are converging on the same domain?

The system should feel like a **control plane for parallel
development**, not a monitoring tool.

---

## 3. Support Unstructured Inputs by Default

The environment cannot enforce:

- Spec templates
- Naming conventions
- Structured task definitions
- Formal metadata

Therefore the platform must rely on:

- Automated inference
- Retrieval-based grounding
- LLM semantic extraction
- Confidence scoring

Structure is derived --- never required.

---

## 4. Deliver Immediate Value to the Single Developer

The first successful deployment target is:

> One developer managing 5--10 simultaneous agent workstreams.

If the system removes sequencing burden and improves awareness at this
scale, it naturally extends to team-level coordination.

---

# Foundational Mental Model

Treat all ongoing work as **Change Streams** --- continuously inferred
units of intent derived from heterogeneous signals such as:

- Local diffs
- Branches
- Agent conversations
- Generated artifacts
- Open files
- Commit drafts
- Scratch documents

Each stream is transformed into a machine-comparable object called a
**Change Signature**.

---

# The Change Signature (Canonical Representation)

Every stream should be automatically summarized into a structured
signature containing:

## Intent

A stable semantic description of what the change is attempting to
accomplish.

## Typed Touchpoints

Not just files, but:

- Symbols
- APIs / endpoints
- Database objects
- Configuration keys
- Domain concepts

## Impact Neighborhood

A compressed representation of nearby dependencies and related
subsystems.

## Order Constraints

Inferred relationships such as:

- Must land before/after another change
- Alters shared contracts
- Depends on schema or interface modifications

## Confidence Vector

Explicit uncertainty estimates so the system can distinguish inference
from high-certainty signals.

---

# Required Repository Understanding Layers

To support reliable inference, construct multiple machine-first views of
the codebase.

## Semantic Layer

Embeddings over code, configs, schemas, and documentation to enable
meaning-based retrieval.

## Structural Layer

Dependency graphs including:

- Imports
- Symbol references
- Service boundaries
- API routes
- Schema usage

## Evolution Layer

Patterns of historical change that reveal natural work regions.

## Expertise Layer

Authorship distributions that identify likely context holders.

These layers function as retrieval infrastructure for the LLM rather
than human-facing diagrams.

---

# Awareness Outputs for Developers

## Portfolio View

A continuously updated snapshot of all active streams showing:

- Intent
- Major touchpoints
- Dependencies
- Freshness
- Confidence

## Landing Sequence Planner

Automatic proposal of partial ordering across streams to reduce merge
friction and design conflicts.

## Stream Similarity Detection

Identification of converging efforts that may benefit from
consolidation.

## Expertise Routing

Context suggestions indicating who is most familiar with impacted
regions.

This is coordination intelligence --- not alerting.

---

# Core Technical Strategy

## Use LLMs For:

- Intent extraction from messy artifacts
- Entity typing and normalization
- Relationship inference
- Semantic comparison

## Do NOT Use LLMs As The Sole Source Of Truth

Always ground inference with retrieval from repository indices.

**Pattern:** Retrieval → LLM reasoning → confidence scoring.

---

# Priority Investment Areas

## 1. Change Stream Detection

Automatically identify concurrent work from local artifacts.

This is the highest-leverage capability.

---

## 2. Signature Extraction Pipeline

Transform heterogeneous inputs into stable, comparable signatures.

Without this layer, higher-order reasoning is impossible.

---

## 3. Touchpoint Normalization

Map raw edits to meaningful architectural entities.

Accuracy here directly determines system trust.

---

## 4. Landing Order Inference

Provide sequencing intelligence that eliminates mental bookkeeping for
developers.

This delivers immediate productivity gains.

---

## 5. Cross-Stream Affinity Modeling

Compute multi-axis relatedness across streams to guide coordination
organically.

---

# Design Principles

## Machine-First Representations

Optimize for computational comparison rather than human visualization.

## Derived Structure Over Enforced Structure

Inference must tolerate incomplete and chaotic inputs.

## Awareness Over Alerts

Promote understanding instead of triggering warnings.

## Confidence Transparency

The system should communicate when it is guessing.

## Incremental Intelligence

Ship early with partial understanding; refine continuously.

---

# Definition of Success

The system is successful when:

- Developers no longer track sequencing mentally
- Parallel agent work becomes safely scalable
- Design conflicts are identified before code lands
- Collaboration begins earlier and more naturally
- Organizational awareness emerges from the same substrate

---

# Strategic Outcome

This platform becomes a **development awareness layer** --- a
coordination fabric sitting above version control that understands not
just what code _is_, but what work is _becoming_.

It transforms parallel, agent-driven development from a cognitive burden
into a scalable operating model.

---

# Repository Analysis Architecture

## Analysis Layer Stack

```
┌─────────────────────────────────────────────────────────┐
│  COARSE (language-agnostic)                             │
├─────────────────────────────────────────────────────────┤
│  1. Directory Topology                                  │
│     - Path hierarchy, naming patterns                   │
│     - Config file locations (package.json, go.mod...)   │
│                                                         │
│  2. Git Co-Change Clusters                              │
│     - Files that change together historically           │
│     - Commit coupling strength                          │
│     - Author overlap                                    │
│                                                         │
│  3. File Similarity (Semantic)                          │
│     - Embedding-based clustering                        │
│     - Content fingerprinting                            │
├─────────────────────────────────────────────────────────┤
│  FINE (language-aware, pluggable)                       │
├─────────────────────────────────────────────────────────┤
│  4. Import/Dependency Graph                             │
│     - Static import analysis per language               │
│     - Cross-file references                             │
│                                                         │
│  5. Symbol Graph                                        │
│     - Functions, types, exports                         │
│     - Call relationships                                │
│     - Symbol → file mapping                             │
└─────────────────────────────────────────────────────────┘
```

---

## Language-Agnostic Strategy

**Layers 1-3**: Pure language-agnostic. Git + paths + embeddings work on any repo.

**Layers 4-5**: Pluggable adapters. Start with:

- Go (tree-sitter or `go/ast`)
- TypeScript/JavaScript (tree-sitter)
- Fallback: LLM-based extraction for unsupported languages

Tree-sitter gives us a unified AST interface across ~40 languages, which keeps the core logic language-agnostic while adapters handle parsing.

---

## Output Schema for LLM Retrieval

```yaml
repository:
  regions:
    - id: 'auth-subsystem'
      description: 'Authentication and session management'
      confidence: 0.85
      boundaries:
        directories: ['internal/auth/', 'pkg/session/']
        files: ['cmd/login.go', 'middleware/jwt.go']
      coupling:
        internal: 0.92 # files change together
        external:
          - region: 'api-handlers'
            strength: 0.67
      symbols:
        key_types: ['Session', 'TokenClaims', 'AuthMiddleware']
        key_functions: ['ValidateToken', 'RefreshSession']

  file_index:
    'internal/auth/token.go':
      region: 'auth-subsystem'
      imports: ['crypto/jwt', 'internal/config']
      exports: ['ValidateToken', 'GenerateToken']
      co_change_neighbors: ['internal/auth/session.go', 'api/login.go']
      last_authors: ['alice', 'bob']
```

This gives LLMs:

- **Region-level** context for high-level reasoning
- **File-level** for specific queries
- **Coupling data** for impact analysis
- **Confidence scores** throughout

---

## Build Order

1. **Git co-change analysis** - highest signal, zero language deps
2. **Directory topology** - simple heuristics, immediate value
3. **Tree-sitter symbol extraction** - unified approach for fine-grained
4. **Embedding clustering** - requires embedding infra, defer slightly
5. **Region inference** - LLM-assisted clustering of the above

---

# Workspace Analysis

## From Repository Map to Workspace Analysis

The repository analysis creates a **static map** of the codebase. Each workspace then gets analyzed as a **delta on that map**.

```
┌─────────────────────────────────────────────────────────┐
│  Repository Analysis (computed once, cached)            │
│  - Regions, coupling graph, file index                  │
└───────────────────────────┬─────────────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  Workspace A  │   │  Workspace B  │   │  Workspace C  │
│  (branch)     │   │  (branch)     │   │  (branch)     │
└───────┬───────┘   └───────┬───────┘   └───────┬───────┘
        │                   │                   │
        ▼                   ▼                   ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  Change       │   │  Change       │   │  Change       │
│  Signature A  │   │  Signature B  │   │  Signature C  │
└───────────────┘   └───────────────┘   └───────────────┘
```

---

## Workspace Analysis Pipeline

For each workspace:

### 1. Collect Raw Signals

- `git diff main...HEAD` (committed changes on branch)
- `git diff HEAD` (uncommitted changes)
- Spec files if present (`.spec.md`, `PLAN.md`, etc.)
- Optionally: conversation/agent context

### 2. Map Changes to Repository Regions

- Which files changed → which regions are touched
- Which symbols modified → finer-grained impact
- Use coupling graph to find **affected neighbors** (files not changed but coupled to changed files)

### 3. Generate Change Signature

```yaml
workspace: 'schmux-003'
branch: 'feature/change-awareness'

intent: 'Add repository analysis for detecting parallel work conflicts'
confidence: 0.9 # high if spec file exists, lower if inferred from diffs

touchpoints:
  direct:
    regions: ['analysis-subsystem']
    files: ['internal/analysis/repo.go', 'internal/analysis/cochange.go']
    symbols: ['AnalyzeRepository', 'CoChangeCluster']

  affected: # via coupling graph
    regions: ['dashboard-api']
    files: ['internal/dashboard/handlers.go']
    reason: 'handlers.go historically changes with analysis code'

constraints:
  requires_schema_change: false
  touches_public_api: true
  breaking_change_risk: low
```

---

# Comparison Analysis (Cross-Workspace)

Given Change Signatures for all active workspaces, compute pairwise relationships.

## Overlap Detection

```
Workspace A touches: [auth, session, middleware]
Workspace B touches: [session, api-handlers]
                          ↑
                     OVERLAP: session
```

Types of overlap:

| Type         | Description                                   |
| ------------ | --------------------------------------------- |
| **Direct**   | Same files modified                           |
| **Regional** | Same logical region, different files          |
| **Coupled**  | Different regions but historically coupled    |
| **Semantic** | Different code, similar intent (LLM-detected) |

---

## Ordering Constraints

```yaml
comparisons:
  - pair: [workspace-A, workspace-B]
    overlap:
      type: 'direct'
      files: ['internal/session/manager.go']
      symbols: ['SessionManager.Refresh']

    ordering:
      suggested: 'A before B'
      reason: 'A modifies interface that B consumes'
      confidence: 0.8

    risk: 'medium'
    recommendation: 'Coordinate on SessionManager interface before B proceeds'
```

---

## Affinity Scoring

Not just "do they conflict" but "how related are they":

| Affinity Type       | Signal                      | Meaning                            |
| ------------------- | --------------------------- | ---------------------------------- |
| File overlap        | Same files touched          | Definite coordination needed       |
| Region overlap      | Same logical area           | Likely design interaction          |
| Coupling proximity  | Neighbors in coupling graph | Potential ripple effects           |
| Semantic similarity | Similar intent embeddings   | Possible duplication of effort     |
| Author overlap      | Same historical authors     | Same person might want to sequence |

---

## Output for schmux Dashboard

```yaml
workspace_relationships:
  - workspaces: ['schmux-001', 'schmux-003']
    status: 'safe_parallel'
    overlap: none
    note: 'Different subsystems, no coupling detected'

  - workspaces: ['schmux-002', 'schmux-004']
    status: 'coordinate'
    overlap:
      type: 'regional'
      region: 'api-handlers'
    suggested_order: 'schmux-002 first'
    reason: '002 adds endpoint, 004 modifies response format'

  - workspaces: ['schmux-003', 'schmux-005']
    status: 'potential_conflict'
    overlap:
      type: 'direct'
      files: ['internal/config/config.go']
    risk: 'high'
    recommendation: 'Merge 003 before 005 continues'
```

---

## The Key Insight

The repository analysis gives us a **coordinate system** - regions, coupling, symbols.

Workspace analysis plots each workspace's changes onto that coordinate system.

Comparison is then **geometric** - measuring distance and overlap in that space, not just string-matching file paths.

This means we can detect:

- "These workspaces touch different files but the same logical concern"
- "These files look unrelated but historically break together"
- "This workspace's changes will affect code the other workspace depends on"

---

# Implementation Plan

## Phase Overview

| Phase | Name                | Output                                 | Checkpoint                                             |
| ----- | ------------------- | -------------------------------------- | ------------------------------------------------------ |
| 1     | Repository Analysis | `RepoIndex` JSON for any repo          | Review index quality, coupling accuracy                |
| 2     | Workspace Analysis  | `ChangeSignature` per workspace        | Review signature completeness, affected file detection |
| 3     | Comparison          | `ComparisonReport` for workspace pairs | Review classification accuracy, usefulness             |

---

## Phase 1: Repository Analysis

### Goal

Build a static index of a repository that captures file relationships, coupling patterns, and package boundaries.

### Components

| Component           | Description                                                                         |
| ------------------- | ----------------------------------------------------------------------------------- |
| `CoChangeAnalyzer`  | Parse `git log --name-only`, compute coupling scores between file pairs             |
| `DirectoryAnalyzer` | Walk file paths, identify package/module boundaries from structure and config files |
| `RepoIndexer`       | Combine analyzers into single `RepoIndex` output                                    |

### Deliverables

1. CLI command: `schmux analyze-repo <repo-name>` calls daemon API and produces `repo-index.json`
2. API endpoint performs analysis server-side, writes index file in the resolved local workspace checkout
3. Coupling matrix with configurable history depth

### Data Structures

```go
type RepoIndex struct {
    RepoPath   string                    `json:"repo_path"`
    AnalyzedAt time.Time                 `json:"analyzed_at"`
    Files      map[string]FileInfo       `json:"files"`
    Coupling   map[string][]CoupledFile  `json:"coupling"`
    Packages   []Package                 `json:"packages"`
}

type FileInfo struct {
    Path    string   `json:"path"`
    Package string   `json:"package"`
    Authors []string `json:"authors"`
}

type CoupledFile struct {
    Path     string  `json:"path"`
    Strength float64 `json:"strength"`  // 0-1
}

type Package struct {
    Name       string   `json:"name"`
    Path       string   `json:"path"`
    Files      []string `json:"files"`
    ConfigFile string   `json:"config_file,omitempty"`  // go.mod, package.json, etc.
}
```

### Checkpoint 1 Criteria

- [ ] Can run on schmux repo and produce valid JSON
- [ ] Coupling scores make intuitive sense (files that change together score high)
- [ ] Package detection works for Go and JS/TS projects
- [ ] Performance acceptable for repos with 1000+ commits

---

## Phase 2: Workspace Analysis

### Goal

Given a workspace directory (with local changes), produce a Change Signature that maps those changes onto the repository index.

### Components

| Component            | Description                                                               |
| -------------------- | ------------------------------------------------------------------------- |
| `DiffCollector`      | Run `git diff main...HEAD` and `git diff HEAD`, extract changed file list |
| `SignatureGenerator` | Map changed files to packages, find coupled neighbors, produce signature  |

### Deliverables

1. CLI command: `schmux analyze-workspace <path>` → produces `change-signature.json`
2. Requires `RepoIndex` for the base repo
3. Identifies both direct changes and affected files via coupling

### Data Structures

```go
type ChangeSignature struct {
    Workspace     string         `json:"workspace"`
    Branch        string         `json:"branch"`
    BaseBranch    string         `json:"base_branch"`
    AnalyzedAt    time.Time      `json:"analyzed_at"`

    // Direct changes
    ChangedFiles  []string       `json:"changed_files"`
    TouchedPkgs   []string       `json:"touched_packages"`

    // Indirect impact (via coupling)
    AffectedFiles []AffectedFile `json:"affected_files"`
    AffectedPkgs  []string       `json:"affected_packages"`

    // Stats
    Insertions    int            `json:"insertions"`
    Deletions     int            `json:"deletions"`
}

type AffectedFile struct {
    Path     string  `json:"path"`
    Reason   string  `json:"reason"`   // "coupled to X"
    Strength float64 `json:"strength"`
}
```

### Checkpoint 2 Criteria

- [ ] Can analyze any schmux workspace
- [ ] Correctly identifies changed files (committed + uncommitted)
- [ ] Affected files list is reasonable (not too noisy, not missing obvious connections)
- [ ] Package mapping is accurate

---

## Phase 3: Comparison Analysis

### Goal

Given two Change Signatures, produce a comparison report that classifies the relationship and provides actionable guidance.

### Components

| Component                | Description                                                 |
| ------------------------ | ----------------------------------------------------------- |
| `SignatureComparator`    | Compare two signatures, compute overlap metrics             |
| `RelationshipClassifier` | Classify as safe_parallel / coordinate / potential_conflict |
| `ReportGenerator`        | Produce human-readable and machine-readable output          |

### Deliverables

1. CLI command: `schmux compare-workspaces <path1> <path2>` → produces comparison report
2. Dashboard integration: show workspace relationships in UI
3. JSON output for LLM consumption

### Data Structures

```go
type ComparisonReport struct {
    Workspaces     [2]string       `json:"workspaces"`
    ComparedAt     time.Time       `json:"compared_at"`

    // Classification
    Status         RelationStatus  `json:"status"`
    Risk           RiskLevel       `json:"risk"`

    // Overlap details
    DirectOverlap  []string        `json:"direct_overlap"`
    PackageOverlap []string        `json:"package_overlap"`
    CoupledOverlap []CoupledPair   `json:"coupled_overlap"`

    // Recommendations
    SuggestedOrder string          `json:"suggested_order,omitempty"`
    Recommendation string          `json:"recommendation"`
}

type RelationStatus string

const (
    StatusSafeParallel     RelationStatus = "safe_parallel"
    StatusCoordinate       RelationStatus = "coordinate"
    StatusPotentialConflict RelationStatus = "potential_conflict"
)

type RiskLevel string

const (
    RiskNone   RiskLevel = "none"
    RiskLow    RiskLevel = "low"
    RiskMedium RiskLevel = "medium"
    RiskHigh   RiskLevel = "high"
)

type CoupledPair struct {
    FileA    string  `json:"file_a"`
    FileB    string  `json:"file_b"`
    Strength float64 `json:"strength"`
}
```

### Checkpoint 3 Criteria

- [ ] Classification matches human intuition for test cases
- [ ] False positive rate is acceptable (not crying wolf)
- [ ] Recommendations are actionable
- [ ] Output is useful for both dashboard display and LLM retrieval

---

## Post-MVP Enhancements

After Phase 3 checkpoint passes:

1. **Symbol-level analysis** - Tree-sitter for finer granularity
2. **Embedding clustering** - Semantic similarity without coupling history
3. **LLM-assisted region inference** - Named regions beyond packages
4. **Intent extraction** - Parse spec files and conversations
5. **Continuous monitoring** - Watch workspaces, update signatures automatically
