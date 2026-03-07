# Implementation Plans

Step-by-step implementation plans for features currently being built. Plans are **temporary** — they are deleted once the implementation is complete.

## Lifecycle

1. A design spec exists in `docs/specs/`
2. An implementation plan is created here from the spec
3. The plan is executed task-by-task
4. Once all tasks are done, the plan is deleted
5. The design spec is consolidated into a subsystem guide via `/finalize`

## Rules

- Plans are written for agent consumption — they contain specific file paths, code snippets, and test commands
- Each task in a plan should be independently committable
- Plans should not be committed to `main` — they exist only on feature branches during active development
