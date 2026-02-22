# Definition of Done

## Problem

Agents frequently commit code that fails one or more quality bars:

1. **Tests fail** — tests not run before committing, or wrong command used
2. **Architecture drift** — new bespoke approaches or short-term hacks instead of following established patterns
3. **Tests not written** — implementation lands without corresponding test coverage
4. **Documentation not updated** — `docs/api.md` not updated when API-related packages change, other docs stale

We have a patchwork of enforcement today: a pre-commit hook that formats files, `./test.sh` documented in CLAUDE.md and AGENTS.md, `scripts/check-api-docs.sh` enforced in CI, and commit message instructions in our packaged commit command. None of this constitutes a reliable, hard definition of done.

## What "Done" Means

For this project, done is defined at the **commit boundary** — not at PR open, not at merge. A commit that doesn't meet the definition of done should not exist.

## Scope: Repo-Specific First, Product Later

This spec addresses schmux's own development workflow (agents working in this repo). However, the architecture should anticipate a future product feature: schmux workspaces reading a DoD config from `.schmux/config.json` so that any repo managed by schmux can define and enforce its own definition of done.

Concretely: design the _repo-specific_ solution in a way where the DoD criteria are expressed as configuration rather than hardcoded logic, so the configuration layer is easy to extract later.

## Enforcement Mechanism

The commit command is the chokepoint. Agents commit via a packaged `/commit` command. A project-level override of that command at `.claude/commands/commit.md` can intercept the commit act and enforce DoD checks before `git commit` runs.

This approach:

- **Hard-enforces for agents** — agents use `/commit`; the command runs checks before proceeding
- **Does not block humans** — humans can still run `git commit` directly when they know it's appropriate (e.g., a docs typo, a WIP checkpoint)
- **Is auditable** — the commit command file is in the repo; the DoD criteria are readable and improvable

## The Checks

### Mechanical (automatable)

- Run `./test.sh` — unit tests pass
- If API-related packages changed (`internal/dashboard/`, `internal/config/`, `internal/state/`, `internal/workspace/`, `internal/session/`, `internal/tmux/`), `docs/api.md` must also be modified in the staged changes

### Judgment (agent self-assessment)

- New functionality has corresponding test files
- No new bespoke patterns introduced where existing architecture already handles the problem (e.g., polling added where WebSocket state already exists, new state management approach where SessionsContext is the established pattern)
- Commit message follows the format: short imperative subject, no body padding, co-authorship line

## Three Approaches

### A — Commit Command Only

**What it is:** A project-level `.claude/commands/commit.md` that embeds all DoD logic inline. Mechanical checks run as shell commands. Judgment checks appear as a mandatory self-assessment block the agent must complete before `git commit` is called.

**How it works:**

1. Command runs `./test.sh` — aborts if it fails
2. Command checks staged files against API-package regex — aborts if `docs/api.md` not staged
3. Agent answers a structured checklist (tests written? patterns followed? docs updated?) — aborts if any answer is no
4. Only then: `git add` + `git commit`

**Pros:**

- Single file, easy to audit
- No new concepts to introduce
- Hard enforcement on all mechanical checks

**Cons:**

- DoD definition is embedded in commit mechanics — hard to reuse or reference independently
- When this becomes a product feature, the command file doesn't map cleanly onto configurable per-workspace criteria
- The checklist answers are only as reliable as the agent's honesty under pressure

---

### B — Skill + Commit Command (recommended)

**What it is:** A `definition-of-done` skill that defines what done means as a standalone reference, and a `.claude/commands/commit.md` that invokes it. The skill is the source of truth; the command is the enforcement point.

**How it works:**

1. `/commit` is invoked
2. Command instructs agent: "Invoke the `definition-of-done` skill and complete all checks before proceeding"
3. Skill walks through mechanical checks (runs commands) and judgment checks (structured self-assessment)
4. Skill produces a pass/fail result
5. Command proceeds to `git add` + `git commit` only on pass

**Pros:**

- Clean separation: _what done means_ (skill) vs _when to enforce it_ (command)
- Skill is independently readable and improvable — teammates can review and update it without understanding commit command mechanics
- Maps directly onto the product extension: the skill content becomes what each workspace's `dod` config block drives
- Skills are auto-invoked by the superpowers framework when description matches — could be surfaced in other contexts (e.g., before opening a PR)

**Cons:**

- Two files to maintain vs one
- Requires agents to use the superpowers framework (already a project dependency)

---

### C — Script + Skill + Commit Command

**What it is:** Adds `scripts/check-done.sh` as a standalone mechanical-check runner, callable from CI, hooks, or manually. The skill handles judgment calls. The command orchestrates both.

**How it works:**

1. `/commit` invokes `scripts/check-done.sh` — runs all mechanical checks, exits nonzero on failure
2. Command invokes `definition-of-done` skill for judgment checks
3. On both passing: `git add` + `git commit`

**Pros:**

- Most extensible toward the product vision: the script can become a daemon-callable check
- CI can run the same check script independently of the agent workflow
- Cleanest separation of mechanical vs judgment concerns

**Cons:**

- Most moving parts for a repo-specific problem today
- `scripts/check-done.sh` would largely duplicate what `./test.sh` and `scripts/check-api-docs.sh` already do
- Premature — build this if/when schmux's product DoD feature actually ships

---

## Recommendation: Approach B

The skill + commit command split solves today's problem cleanly and provides a clear migration path to the product feature. The skill becomes the unit that a future `dod` config block in `.schmux/config.json` would populate and override; the commit command's enforcement logic stays stable.

## Future: Product Extension

When this becomes a product feature, the shape would be:

```json
// .schmux/config.json
{
  "dod": {
    "before_commit": {
      "run": ["./test.sh"],
      "require_docs_update": {
        "when_changed": ["internal/dashboard/", "internal/config/"],
        "update": ["docs/api.md"]
      },
      "checklist": [
        "New functionality has corresponding tests",
        "No new patterns introduced where existing architecture applies"
      ]
    }
  }
}
```

The dashboard could surface DoD status per session, and the session manager could gate commits (or warn) based on this config.

## Open Questions

1. Should the judgment self-assessment be structured (yes/no per item) or free-form? Structured is harder to rationalize around.
2. Should a failed DoD check abort the commit entirely, or produce a warning the agent must explicitly override with a reason? (Abort is harder enforcement; override-with-reason creates an audit trail.)
3. For the product version: should DoD config live in `.schmux/config.json` (repo-level) or be set per-workspace via the dashboard?
