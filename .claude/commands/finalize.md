---
allowed-tools: Bash(git status:*), Bash(git diff:*), Bash(git log:*), Bash(git add:*), Bash(git rm:*), Bash(git branch:*), Bash(mv *), Read, Glob, Grep, Edit, Write, Task, AskUserQuestion, Skill
description: Consolidate design specs into subsystem guides after a feature is implemented
---

## What This Command Does

After a feature is fully implemented, this command consolidates its design spec(s) into a subsystem guide that helps future agents navigate the codebase. It transforms proposal-style specs ("we should build X") into reference-style guides ("X works like this because...").

## Arguments

- `$ARGUMENTS` — Optional. One or more spec file paths to consolidate. If empty, run discovery.

## Process

### Step 1: Discover Specs to Consolidate

**If spec paths were provided as arguments:** Use those. Skip to Step 2.

**If no arguments:** Discover candidates:

1. Check the current branch name and recent commits (`git log --oneline -20`) to understand what was recently implemented.
2. List all files in `docs/specs/` and `docs/plans/` and read the first 10 lines of each to understand what they describe.
3. Cross-reference: which specs or plans describe features that appear in recent commits or exist in the codebase as implemented code?
4. Present the candidates to the user:
   - "These specs appear to be fully implemented: [list]. Which should I consolidate?"
   - Also flag any docs in `docs/` that look like one-time migration guides or historical reports (e.g., migration guides for completed migrations, coverage reports). Ask: "These docs appear to be historical artifacts. Delete them?"
5. Wait for user confirmation before proceeding.

### Step 2: Analyze Each Spec

For each spec to consolidate:

1. Read the full spec.
2. Identify the key packages/files it describes by searching the codebase.
3. Read the actual code to understand what was implemented vs. what the spec proposed.
4. Extract the valuable content:
   - **Architecture decisions** — the "why" behind design choices, rejected alternatives, trade-offs
   - **Key files** — which files are central to this subsystem
   - **Gotchas** — non-obvious pitfalls, things that look like they should work one way but don't, common mistakes
   - **Common modification patterns** — how a developer would extend or modify this area
5. Check if other specs in `docs/specs/` or plans in `docs/plans/` relate to the same subsystem. If so, ask the user: "These docs also relate to this area: [list]. Include them in this consolidation?"

### Step 3: Find or Create the Subsystem Guide

1. Check `docs/` for an existing guide that covers this subsystem.
2. If one exists, read it — the new content will be merged into it.
3. If none exists, create a new file in `docs/` with a descriptive name (e.g., `docs/remote-access.md`, `docs/authentication.md`).

The agent decides the natural grouping. Don't force a 1:1 mapping between specs and guides — multiple specs may consolidate into one guide, and one spec may touch content in multiple guides.

### Step 4: Write the Guide

Write or update the subsystem guide following this template:

```markdown
# [Subsystem Name]

## What it does

1-2 sentences. What problem this subsystem solves.

## Key files

| File                      | Purpose                   |
| ------------------------- | ------------------------- |
| `internal/foo/manager.go` | Main lifecycle management |
| `internal/foo/handler.go` | HTTP API handlers         |

## Architecture decisions

- Why X instead of Y (context from the spec)
- Why Z is structured this way (trade-off explanation)

## Gotchas

- Don't do X — it looks right but breaks because Y
- This field is nested under `sessions` config, not top-level
- The WebSocket message type is "sessions" not "dashboard" despite what you'd expect

## Common modification patterns

- To add a new [thing]: touch these files, follow this pattern
- To change [behavior]: start at this file, update these tests
```

**Key principles:**

- Write in present tense ("the system uses..."), not future tense ("we should build...")
- Point to files, don't duplicate code. The guide is a launchpad for reading source.
- Preserve the "why" — this is the main value specs carry that code comments don't.
- Gotchas should be short, direct warnings. Things an agent can't discover from code alone.
- Keep it concise. If a section would exceed ~10 bullet points, it's too detailed.
- Don't include implementation plans, task lists, or step-by-step build instructions.

### Step 5: Clean Up

1. Delete the consolidated spec(s) from `docs/specs/` and/or plan(s) from `docs/plans/` using `git rm`.
2. Delete any historical docs the user approved for removal.
3. Stage the new/updated guide(s) and the deletions.

### Step 6: Commit

Use `/commit` to create the commit. Suggest this message format:

```
docs: consolidate [spec-name] into [guide-name] subsystem guide
```

If multiple specs were consolidated:

```
docs: consolidate [N] specs into [guide-name] subsystem guide
```

## Important Notes

- Always ask the user before deleting anything. Never silently remove docs.
- If a spec describes something that was NOT implemented (or was implemented differently), note the discrepancy in the guide under "Architecture decisions" or "Gotchas" rather than documenting the unimplemented design.
- If an existing guide in `docs/` already covers a topic well, prefer updating it over creating a new file.
- This command does NOT run tests or check for behavioral changes — it only touches documentation.
