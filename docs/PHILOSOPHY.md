# schmux Philosophy

The major problems of developing software with agentic coding, and how schmux helps.

---

## 1. General Theory

Coding agents are incredibly powerful and allow code to become cheap, but they do not change the cost of software. [Code Is Cheap Now. Software Isn't.](https://www.chrisgregori.dev/opinion/code-is-cheap-now-software-isnt)

The fundamental challenge: you need to balance letting the user streamline as much as possible while staying able to step in and review or make changes at any point. The user is in control as [Clerky tweets](https://x.com/bcherny/status/2007179832300581177) about how the author of Claude Code develops.

Don't overcomplicate the dev environment. Avoid Rube Goldberg scaffolding — your development environment is a product, and every script, harness, and loop you add is a "feature" that requires maintenance and adds technical debt. [Your Dev Environment Should Also Not Be Overcomplicated](https://adventurecapital.substack.com/p/your-dev-environment-should-also)

English is becoming a programming language. Prompts are load-bearing infrastructure that get submitted to version control and treated as code. This has surprising non-obvious consequences for how we build and maintain systems. [English as a Programming Language](https://linotype.substack.com/p/english-as-a-programming-language)

---

## 2. Non-Goals

schmux is deliberately *not* trying to be:

- **A cloud service** — schmux runs locally on your machine. No hosted infrastructure, no accounts, no data leaving your filesystem.
- **Agent-to-agent orchestration** — schmux doesn't have agents coordinating with each other autonomously. The human is always the coordinator.
- **A sandbox or container system** — workspaces are real directories on your actual filesystem, not isolated Docker containers or VMs.
- **Batch/headless automation** — sessions are interactive by design. If you want fully autonomous pipelines, use CI/CD.
- **A replacement for the agents** — schmux orchestrates Claude, Codex, Gemini, etc. It doesn't try to be an AI coding tool itself.
- **An IDE** — it launches VS Code but doesn't try to replace it. The dashboard is for orchestration and observability, not editing.
- **Distributed** — everything runs on one machine. No multi-node coordination or remote session management.
- **Trying to hide git** — git is the foundation, not an implementation detail. You're expected to understand branches, commits, and diffs.

---

## 3. Managing Workspaces

**Problem:** Running multiple agents in parallel means managing multiple copies of your codebase. Creating git clones is tedious, keeping them organized is error-prone, and it's easy to lose track of uncommitted work or forget which workspace has what changes.

- **Git as the primary organizing format**: workspaces are actual git repositories on your filesystem
- **Filesystem-based, not containerized**: using your actual file system rather than docker or some more abstracted way to have lots of copies of git repos
- **Workspace overlays**: auto-copy local-only files (.env, configs) that don't belong in git but are important to run the app
- **Git status visualization at a glance**: dirty indicator, branch name, commits ahead/behind
- **Diff viewer**: side-by-side git diffs to see what changed
- **VS Code launcher**: one click to launch a VS Code window in this workspace
- **Safety checks**: cannot dispose workspaces with uncommitted or unpushed changes

---

## 4. Multi-Agent Coordination

**Problem:** The agentic coding landscape is fragmented—Claude, Codex, Gemini, and more. Each has strengths. Locking into one vendor limits your options, and switching between tools manually is friction that slows you down.

- **Multiple agents can work in the same workspace simultaneously**
- **Unopinionated about what you run**: support a wide variety of tools
- **Out-of-box support** for Claude, Codex, and Gemini; will add more over time
- **Variants**: different endpoints/models of the same tool
- **User-defined run targets**: supply your own CLI scripts for tools we don't support
- **Quick launch presets**: saved combinations of target + prompt for fully automated, non-prompt-needing actions
- **Cookbooks**: reusable prompt libraries for common tasks

---

## 5. Sessions

**Problem:** Most agent orchestration focuses on agents talking to each other, batch operations, and strict sandboxes. This makes it hard for *you* to see what's happening or step in when needed. For long-running agent work, you need a lightweight, local solution where you can observe, review, and interject at any point—with sessions that persist if you disconnect, preserve history, and can be reviewed after completion.

- **Each coding agent runs interactively in its own tmux session**
- **Session lifecycle states**: spawning → running → done → disposed
- **Sessions persist** after process exits for review
- **Attach via terminal anytime**: `tmux attach -t <session>`

**Problem:** Now you've got a dozen concurrent sessions. You don't want to spend your day clicking into each terminal to figure out what's happening. You need to know at a glance: which are still working, which are blocked, which are done, which you've already reviewed, and where to focus your attention next.

- **Dashboard shows real-time terminal output** via WebSocket
- **Last activity**: when the agent last produced output
- **When you last viewed**: timestamp of when you last looked at the session
- **NudgeNik**: summarizes the agent's state — blocked, wants feedback, working, or done

**Problem:** Even with visibility, there's grunt work—spinning up sessions, creating workspaces, typing the same prompts. These small tasks steal attention from the actual problem you're trying to solve.

- **Bulk create sessions**, across the same or new workspaces
- **On-demand workspace creation** when spawning
- **Nicknames** for easy identification

---

## 6. CLI and Web Support

**Problem:** Some tasks are faster from a terminal; others benefit from visual UI. Tools that force you into one interface create friction when the other would be better for the job.

- **Web dashboard**: observability and orchestration
- **CLI commands**: daemon management (start, stop, status, daemon-run), session operations (spawn, list, attach, dispose), workspace overlays (refresh-overlay)
- **Connection status indicator**: always know if you're connected to daemon
- **Status-first design**: running/stopped/error visually consistent everywhere
- **URL idempotency**: routes bookmarkable, survive reload
- **Real-time updates**: preserve scroll position, no jarring refreshes
- **Spawn wizard**: multi-step form for repo/branch/target/prompt
- **Destructive actions slow**: "Dispose" requires confirmation

---

## 7. NudgeNik: A Glimpse of the Future

**Problem:** Coding agents and LLMs are inherently powerful *because* they aren't binary and can operate in ambiguous spaces. But most orchestration tools are attempting to squash that ambiguity rather than recognize that software development is ambiguous — it's messy, requires judgment, and isn't reducible to binary pass/fail metrics.

**What NudgeNik does today:** It reads what agents recently did and concludes what they're up to. This is valuable right now to tell you whether an agent is blocked (needs permission to run a command or approve a change), waiting for feedback or has clarifying questions, done with everything, or still actively working.

**Where this is going:** Using an LLM to read the English output of coding agents opens the door for more human-centric agent organization. Instead of creating strict orchestration that requires very clear goals, we recognize that software development is messy and requires interpretation.

NudgeNik can grow to:
- **Evaluate what agents are doing and suggest next steps** — Did they actually run the tests they claimed? Do they need integration testing? Did they finish the requirements?
- **Ask (almost rhetorical) questions** when agents are stuck or looping on a problem
- **Suggest seeking other expertise** or trying a different model to think differently when progress has stalled

The future isn't binary orchestration — it's interpretation and judgment.

---

## Relevant Links

- [Code Is Cheap Now. Software Isn't.](https://www.chrisgregori.dev/opinion/code-is-cheap-now-software-isnt) — Chris Gregori
- [Your Dev Environment Should Also Not Be Overcomplicated](https://adventurecapital.substack.com/p/your-dev-environment-should-also) — Ben Mathes
- [English as a Programming Language](https://linotype.substack.com/p/english-as-a-programming-language) — Stefano Mazzocchi
- [Clerky's tweets on Claude Code development workflow](https://x.com/bcherny/status/2007179832300581177)
