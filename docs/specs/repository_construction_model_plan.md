# Repository Construction Model (RCM)

## Agent Build Plan

This document defines the **actionable plan** agents must follow to
analyze a repository as a living construction system rather than a
static code artifact.

The outcome is a **single machine-readable artifact**:

> **`<repo_name>.json` --- Repository Construction Model**

All analysis outputs must be consolidated into this JSON structure. No
fragmented reports.

---

# Objective

Construct a model that answers:

- How is this system organized by capability?
- Where does coordination naturally occur?
- Where will future work likely intersect?
- Where does architectural reality diverge from intent?
- How is the system evolving?

This is **construction intelligence**, not workspace inspection.

---

# Non-Negotiable Constraints

Agents MUST:

- Organize analysis around **capabilities and coordination surfaces**,
  not files.
- Anchor every claim to concrete code artifacts.
- Prefer inference over declared structure.
- Produce machine-readable output only.
- Include confidence signals.

Agents MUST NOT:

- Center analysis on folders or class diagrams.
- Produce tactical file walkthroughs.
- Emit prose without structured anchors.

---

# Final Deliverable

Agents produce exactly one artifact:

## `<repo_name>.json`

Top-level schema:

```json
{
  "repo_summary": {},
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

Each section is required unless explicitly impossible, in which case
agents must explain why.

---

# Analysis Pipeline

Agents must execute the following passes in order.

---

## Pass A --- Inventory & Entry Points

### Goal

Determine what kind of system this is and where execution begins.

### Tasks

Identify:

- Deployable services/apps
- API entrypoints (routes, controllers, RPC handlers)
- Workers, jobs, schedulers
- UI entrypoints (if present)
- Database systems and migration tooling
- Messaging/event infrastructure

### Output Fields

```json
"runtime_components": [
  { "name": "", "type": "", "anchors": [] }
],
"entrypoints": [
  { "type": "api|worker|ui|event", "anchor": "", "notes": "" }
]
```

### Stop Condition

A developer can trace how a request or event enters the system.

---

## Pass B --- Capability Mining (Primary Coordinate System)

### Goal

Infer what the system **does**, not how it is organized.

### Method

Extract domain nouns from:

- Routes / RPC methods
- Database schemas
- Public types
- Package names
- Tests
- Documentation

Cluster them into **8--20 capabilities**.

Avoid generic buckets such as:

- utils
- common
- shared
- misc

Capabilities must describe real business or platform functions.

### Output

```json
"capabilities": [
  {
    "id": "",
    "name": "",
    "description": "",
    "keywords": [],
    "anchors": {
      "entrypoints": [],
      "modules": [],
      "schema": [],
      "symbols": []
    }
  }
]
```

### Stop Condition

A developer can answer: "What are the real subsystems of this product?"

---

## Pass C --- Coordination Surfaces (Contracts / Pressure Zones)

### Goal

Find structures that force sequencing and collaboration.

### Contract Types

- API schemas
- Shared models / DTOs
- Database schemas
- Events / messages
- Feature flags
- Auth policies
- Widely imported libraries
- Configuration keys

### Pressure Heuristics

Estimate coordination pressure using:

- fan-in / reference count
- cross-capability usage
- schema centrality

### Output

```json
"contracts": [
  {
    "id": "",
    "type": "",
    "anchor": "",
    "used_by_capabilities": [],
    "fan_in": 0,
    "notes": ""
  }
]
```

### Stop Condition

Agents can explain: "If this changes, what else must change?"

---

## Pass D --- Reality Map (Actual System Behavior)

### Goal

Model how the system behaves in practice.

### Build Two Graphs

**Structural Graph** - imports - symbol references - schema usage

**Evolution Graph** - co-change clusters from version history

### Identify

- cross-capability clusters
- hidden coupling
- coordination knots
- cyclic dependencies

### Output

```json
"clusters": [
  {
    "id": "",
    "type": "structural|evolutionary|hybrid",
    "members": [],
    "capabilities_involved": []
  }
],
"couplings": [
  {
    "capability_a": "",
    "capability_b": "",
    "strength": "",
    "evidence": []
  }
]
```

---

## Pass E --- Architectural Drift

### Goal

Detect mismatches between intended structure and emergent behavior.

### Compare

Declared boundaries: - services - folders - architecture docs

Observed behavior: - cross-clusters - shared contracts - co-change

### Output

```json
"drift_findings": [
  {
    "declared_boundary": "",
    "observed_behavior": "",
    "impact_on_parallel_work": "",
    "anchors": []
  }
]
```

### Stop Condition

Agents can answer: "Where will developers be surprised by coupling?"

---

## Pass F --- Change Gravity & Construction Trajectory

### Goal

Predict where future work will land.

### Analyze

- churn by capability
- churn by contract
- emergence of new endpoints/tables/types
- expanding vs stabilizing regions

### Output

```json
"gravity": [
  {
    "region": "",
    "type": "capability|contract",
    "signals": [],
    "implication": ""
  }
],
"trajectory": [
  {
    "direction": "",
    "evidence": [],
    "confidence": ""
  }
]
```

### Stop Condition

Agents can state where collisions are most likely to occur in future
work.

---

# Confidence Requirements

Every major section must include confidence signals:

```json
"confidence": {
  "capabilities": "high|medium|low",
  "contracts": "",
  "drift": "",
  "trajectory": ""
}
```

Agents must explicitly distinguish inference from high-certainty
findings.

---

# Operating Model: Multi-Agent Pipeline

Run analysis as parallel specialists rather than one monolithic agent.

Recommended distribution:

- Agent 1 --- Inventory
- Agent 2 --- Capability Mining
- Agent 3 --- Contracts / Pressure
- Agent 4 --- Structural + Evolution Graphs
- Agent 5 --- Drift Detection
- Agent 6 --- Gravity & Trajectory
- Agent 7 --- Integrator (validates anchors, assembles `rcm.json`)

---

# Acceptance Tests

The artifact is successful if a developer can quickly answer:

1.  What are the real capabilities?
2.  Where must coordination occur?
3.  Where does reality contradict architecture?
4.  Where does work naturally concentrate?
5.  If I start a new feature, what adjacent regions will likely move?

Failure to answer any of these indicates tactical analysis instead of
construction modeling.

---

# Guiding Principle

> Analyze the repository as an evolving construction site.

Do not optimize for correctness of structure alone.

Optimize for **predictive coordination intelligence** --- the ability to
understand how the system is being built and where parallel work will
intersect.
