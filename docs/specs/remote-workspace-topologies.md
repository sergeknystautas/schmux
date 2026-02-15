# Remote Workspace Topologies

**Author:** Aaron Farr
**Date:** February 2026
**Status:** Speculative Draft

---

## Motivation

Schmux is local-first. Serge and Stefano run it on their own machines,
where the host IS the dev environment. Remote support landed recently
(RemoteFlavor, tmux control mode, interactive provisioning with 2FA),
but so far "remote" means "schmux on your laptop managing sessions on a
remote host."

This proposal explores the next step: schmux running *on* the remote
machine as the workspace layer for a shared development server, with the
constraint that **remote must be additive -- it can't break or complicate
local workflows.**

The core question: what does schmux need to become a useful workspace
layer on shared remote infrastructure, beyond what it already does for
local use?

---

## What Is Schmux? (Positioning)

As schmux accretes features -- terminal streaming, protocol integration,
IDE links, coordination protocols -- it's worth stating what the product
IS, to avoid building Coder, Zed, or some accidental middle ground.

**Schmux is a headless agent runtime.** The formula:

```
schmux = tmux (persistence) + worktrees (workspace management) + structured agent interface
```

The key properties that distinguish it from adjacent tools:

| Property | schmux | Zed | Coder/DevPod | raw tmux+SSH |
|----------|--------|-----|-------------|--------------|
| Agents survive disconnects | Yes (tmux) | No (agent dies with editor) | Depends on setup | Yes (manual) |
| Headless operation | Yes | No (requires UI) | Yes | Yes (manual) |
| Structured agent data | Via ACP (proposed) | Native agent panel | No | No |
| Multi-user dashboard | Yes | Channels + project sharing | Yes | No |
| Workspace lifecycle | Yes (worktrees, recycling) | No (manual) | Yes (containers) | No |
| Editor-agnostic | Yes | No (is the editor) | Yes | Yes |
| Real-time collaboration | No (dashboard only) | Yes (CRDT buffers) | No | No |

### Zed Comparison

Zed has significant overlap with schmux's trajectory: channels for
team coordination, project sharing for collaborative editing, an agent
panel for structured AI interaction, and ACP support for external
agents. It's tempting to look at Zed and ask "are we rebuilding this?"

The answer is no, because the key distinction is **persistence and
headlessness.** In Zed, agents die when the editor closes. In schmux,
agents outlive any connection -- they run in tmux, the dashboard
observes them, humans come and go. This is the same value proposition
tmux itself provides over a raw terminal: the session persists
independent of the connection.

Schmux is the runtime. Zed (or VS Code, or Emacs) is one possible
frontend. Multiple editors can connect to the same schmux workspace
simultaneously. The dashboard is another frontend -- a monitoring/
orchestration surface, not an editor. These are complementary, not
competing.

Where Zed IS relevant:
- If Zed becomes the team's editor, its collaboration features (CRDT
  shared editing, channels) partially overlap with chaplin's group
  chat and coordination. Worth tracking but not blocking on.
- Zed's ACP support validates the protocol. If Zed users can connect
  to schmux-managed agents via ACP, schmux becomes the backend that
  gives Zed's agents persistence.
- Zed's remote development uses SSH, same as the `editor-info` deep
  links proposed below. The integration path is clear.

---

## Three Layers of "Remote"

There are three distinct remote problems (per Stefano's framing). They
compose but are independently valuable:

```
Layer 3: Chaplin
  My schmux talks to your schmux.
  Coordination, situational awareness, floor manager.
  → See docs/specs/multiplayer-orchestration.md

Layer 2: Remote Schmux
  Schmux runs in the cloud / on a shared server.
  Dashboard accessible from phone, laptop, anywhere.
  Shared API keys, shared costs, multiple users.
  → Deployment via container images (e.g., agentboxes)

Layer 1: Remote Host
  The agent works on another machine.
  My schmux (local or cloud) spawns sessions on a beefy GPU box.
  → RemoteFlavor handles this today.
```

You can have all three: two cloud schmux instances, each using remote
hosts, using chaplin to communicate between them.

This proposal primarily addresses **Layer 2** (schmux as a shared remote
service) and the workspace-layer concerns that emerge when schmux lives
on a shared machine with multiple projects and multiple users.

---

## Trust Model

This proposal assumes **trusted teams on shared hardware**. Everyone on
the machine can see everyone's workspaces and sessions. No per-user
permissions, namespace isolation, or resource quotas.

This is NOT multi-tenant. Multi-tenant would require auth per workspace,
filesystem isolation (containers at minimum), network policy, audit
logging -- that's Coder/Gitpod/DevPod territory, a different product.

The trusted-team model is a sweet spot those platforms don't serve well.
Coder gives full isolation but heavy overhead. Raw SSH gives shared
hardware but no coordination. Schmux sits in the middle: shared
visibility, lightweight workspaces, just enough structure.

---

## Workspace Topologies

Schmux doesn't need to pick one model. Different setups have different
needs:

### Local, Single-User (Serge, Stefano today)

The host is the dev environment. No environment isolation needed. The
interesting problems are git workflow: branching model, stacked diffs,
workspace recycling, disposal safety.

**Schmux's role:** Workspace lifecycle + git workflow + agent session
management. What's being built now.

### Remote, Single-Project (Meta OnDemand model)

The remote host is pre-provisioned for one project. The environment is
already correct. Similar to local except you need to get there (SSH,
2FA, provisioning).

**Schmux's role:** Same as local, plus remote host connection management.
`RemoteFlavor` handles this today.

### Remote, Multi-Project

A shared remote machine (homelab, cloud VM, beefy desktop) running
schmux, with multiple projects that have different toolchains. Go 1.22
for project A, Python 3.12 for project B, Node 22 for project C.
Multiple developers connect from their own machines with their own
editors.

**Schmux's role:** Everything above, plus per-workspace environment
activation and IDE connection facilitation. This is what the rest of
this proposal addresses.

### Remote, Containerized (heaviest)

Full isolation per workspace. Different OS base, untrusted code,
conflicting system libraries. Each workspace gets a distrobox/docker
container.

**Schmux's role:** Container lifecycle management per workspace. Deferred
-- only needed when the lighter approaches prove insufficient.

---

## Proposal 1: Per-Workspace Environment Activation

### The Problem

Schmux creates worktrees but all workspaces share the host's tools and
runtimes. On a single-project local machine this is fine. On a shared
remote machine with multiple projects, it's a correctness problem:
different projects need different tool versions, and they need to
coexist without stomping on each other.

This is NOT about security isolation. The trust model is "trusted team."
It's about ensuring `go build` in workspace A uses Go 1.22 while
`go build` in workspace B uses Go 1.23.

### Proposed: `wrapper_command` in repo config

The simplest implementation: a per-repo config field that wraps the
agent spawn command.

```json
{
  "repos": [
    {
      "name": "project-a",
      "url": "git@github.com:team/project-a.git",
      "wrapper_command": "direnv exec {{.WorkspacePath}}"
    },
    {
      "name": "project-b",
      "url": "git@github.com:team/project-b.git",
      "wrapper_command": "nix develop {{.WorkspacePath}}#default --command"
    },
    {
      "name": "project-c",
      "url": "git@github.com:team/project-c.git"
    }
  ]
}
```

When schmux spawns an agent session, if `wrapper_command` is set, it
prepends it to the agent command. Project C has no wrapper -- host
environment, same as today.

This is the same Go template pattern already used by `RemoteFlavor`
for `ConnectCommand`/`ProvisionCommand`. No new abstraction needed.

### Why `wrapper_command` Instead of Auto-Detection

The original version of this proposal suggested auto-detecting `.envrc`
/ `flake.nix` / `Dockerfile` and choosing an environment tier
automatically. This is clever but:

1. Auto-detection adds complexity for a feature most users won't need
   (local single-project use doesn't need wrapping)
2. `direnv exec .` already handles the `.envrc` -> nix -> flake chain
   internally; schmux doesn't need to understand the tiers
3. Explicit config is easier to debug when something goes wrong
4. A string template is trivially simple to implement

If auto-detection proves valuable later, it can layer on top of
`wrapper_command` as a default when the field is empty.

### How It Interacts with IDEs

When a developer SSHs into the remote machine and opens a workspace
directory in their editor, the shell's direnv hook activates
automatically (most shell configs have `eval "$(direnv hook bash)"`
or equivalent). The IDE's terminal gets the same environment as the
agent. No IDE-specific configuration needed for direnv-based or
nix-via-direnv setups.

### Implementation Sketch

In `internal/session/manager.go`, the spawn flow already builds agent
commands. Adding wrapper support:

```go
// Existing: build the agent command
agentCmd := buildAgentCommand(target, workspace, ...)

// New: prepend wrapper if configured
if repo.WrapperCommand != "" {
    wrapper, err := renderTemplate(repo.WrapperCommand, templateVars)
    if err != nil { return err }
    agentCmd = wrapper + " " + agentCmd
}

// Existing: create tmux session with the command
tmux.NewSession(sessionName, workspacePath, agentCmd)
```

Config change in `internal/config/config.go`:

```go
type Repo struct {
    Name           string `json:"name"`
    URL            string `json:"url"`
    BarePath       string `json:"bare_path,omitempty"`
    WrapperCommand string `json:"wrapper_command,omitempty"`
}
```

---

## Proposal 2: IDE Connection Info

### The Problem

Developers can already SSH into the remote machine and open a workspace
folder in their IDE. But schmux doesn't make this easy -- you have to
manually figure out the workspace path, construct the SSH command, and
know which host the workspace is on.

The team consensus is: don't build an editor into schmux. Schmux is
"chat-centric" (dashboard, terminal, agent interaction). For "code-
centric" work, use VSCode Remote, Zed SSH, or your editor of choice.
The job here is making that handoff seamless.

### Proposed: `/api/workspaces/:id/editor-info` Endpoint

Expose the connection metadata editors need:

```json
GET /api/workspaces/schmux-001/editor-info

{
  "workspace_id": "schmux-001",
  "path": "/home/dev/.schmux-workspaces/schmux-001",
  "branch": "feature/env-wrapper",
  "ssh_host": "jambot-dev-1",
  "ssh_user": "dev",
  "environment": "direnv",
  "sessions": [
    {
      "id": "session-abc",
      "target": "claude",
      "status": "working",
      "acp_socket": "/tmp/schmux/sessions/session-abc.sock"
    }
  ],
  "deep_links": {
    "zed": "zed ssh://dev@jambot-dev-1/home/dev/.schmux-workspaces/schmux-001",
    "vscode": "vscode://vscode-remote/ssh-remote+jambot-dev-1/home/dev/.schmux-workspaces/schmux-001"
  }
}
```

The `ssh_host` and `ssh_user` come from a new network config field
(the machine's external hostname, which schmux can't infer):

```json
{
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "ssh_host": "jambot-dev-1",
    "ssh_user": "dev"
  }
}
```

### Dashboard Integration

"Open in..." buttons on workspace cards. The dashboard already knows the
workspace path and branch -- this just constructs the deep link URI and
opens it. Low-effort, high-convenience for the remote workflow.

For local schmux, the deep links would use `localhost` or be hidden
entirely (you're already on the machine). No impact on local UX.

Note: VSCode Remote already has `RemoteFlavor.VSCodeCommandTemplate`
support. This proposal extends that pattern to workspace-level deep
links for any editor.

---

## Proposal 3: ACP Integration

ACP (Agent Client Protocol) is a JSON-RPC 2.0 protocol between IDEs
and coding agents -- the LSP of agent communication. It provides
structured tool call events, agent plans, file change notifications,
session modes, slash commands, and permission requests. Currently
requires stdio transport (client launches agent as subprocess); socket
transport is in discussion.

20+ agents support ACP (Claude Code, Codex, Gemini CLI, Goose, Cline,
etc.) and growing. Major editors support it as clients (Zed, VS Code,
JetBrains, Neovim, Emacs).

There are two levels at which schmux could integrate with ACP, from
minimal to architectural.

### 3a: Facilitation (minimal -- schmux stays out of the way)

In the remote workflow, the IDE and the agent both touch the same files
but don't know about each other:

```
Local machine                    Remote machine
--------------                   --------------
IDE (Zed/Emacs)  --SSH files-->  workspace  <-- agent (tmux session)
Browser          --dashboard-->  schmux     <-- terminal observation
```

ACP would bridge the IDE and agent directly:

```
Local machine                    Remote machine
--------------                   --------------
IDE (ACP client) --SSH tunnel--> agent ACP socket (same process)
IDE (SSH)        --SSH files---> workspace files
Browser          --dashboard---> schmux (terminal stream, unchanged)
```

Schmux's role is minimal facilitation:

1. **Tell the agent where to listen.** Set an env var
   (`SCHMUX_ACP_SOCKET=/tmp/schmux/sessions/{id}.sock`) when spawning
   the session. Agents that support ACP sockets use it; others ignore it.
2. **Expose the socket path.** The `editor-info` endpoint includes the
   ACP socket path so IDEs know where to connect.
3. **Stay out of the way.** The IDE connects directly to the agent's
   socket (via SSH tunnel). Schmux doesn't proxy, translate, or bridge.

This depends on agents supporting ACP over Unix domain sockets (not
just stdio). That's external to schmux. Without it, the existing
workflow still works -- IDE edits via SSH, agent status via dashboard
terminal stream, human coordinates visually.

### 3b: Schmux as ACP Client (bigger -- changes the architecture)

A more ambitious option: schmux itself becomes an ACP client, launching
ACP-capable agents via the protocol instead of (or alongside) tmux
terminal observation.

**What schmux gains from being an ACP client:**

| Current (tmux observation) | With ACP |
|----------------------------|----------|
| NudgeNik LLM screen-reading for agent status | Agent declares status via session modes and plan updates |
| `--<[schmux:state:msg]>--` terminal markers | Structured `session/update` notifications |
| `git status` polling to detect file changes | Tool call events report exactly which files changed and why |
| CLI flag injection for system prompts | `session/prompt` with structured content blocks |
| Keystroke injection for human input | `session/prompt` for structured interaction |
| Inferred tool call history from terminal | Explicit tool call events with kind, status, diffs |

All the fragile inference machinery -- NudgeNik, signal markers, OSC 777
(already abandoned) -- becomes unnecessary for ACP-capable agents. The
protocol provides this data natively.

**The hybrid model:**

```
schmux daemon
  |
  |-- tmux sessions (existing, for any agent)
  |     |-- PTY streaming -> dashboard terminal
  |     |-- NudgeNik / signal extraction (fallback)
  |
  |-- ACP sessions (new, for ACP-capable agents)
        |-- structured events -> dashboard panels
        |     (tool calls, plans, file changes, modes)
        |-- session/prompt -> dashboard chat input
        |-- terminal still available as secondary/debug view
```

For ACP-capable agents, the dashboard evolves from terminal-first to
structured-first: tool calls as expandable cards, file diffs inline,
plan progress as a checklist, a chat input for prompts. The terminal
becomes a collapsible detail view, not the main event.

For everything else (legacy agents, custom scripts, manual terminal
work), tmux sessions work exactly as they do now.

**The tension: control vs observation.**

ACP assumes a controlling client model -- the client launches the agent,
sends prompts, manages the lifecycle. This is fundamentally different
from schmux's current observation model where agents run independently
in tmux and schmux watches.

```
ACP:    Client  ->  controls  ->  Agent
schmux: Agent runs independently  <-  schmux observes  <-  human watches
```

If schmux becomes an ACP client, it shifts from observer to controller.
The agent could still run inside tmux for persistence (survive daemon
restart, allow `tmux attach`), but schmux's primary channel would be
ACP with the PTY as secondary.

This is a meaningful architectural decision. It changes what the
dashboard IS -- from a terminal viewer with status badges to an
agent interaction surface with an optional terminal. That may be the
right evolution, but it's a bigger conversation than "add ACP support."

**What ACP doesn't give you:**

- Real-time PTY streaming (ACP terminals return polled text, not
  character-by-character ANSI streams)
- Observation-only mode (ACP assumes the client controls the agent)
- Multi-agent coordination (ACP is 1 client : N agents, no cross-agent
  awareness)

These gaps are why the hybrid model matters -- tmux handles what ACP
can't, and vice versa.

---

## Proposal 4: MCP Coordination Tools (Speculative)

### The Problem

Agents in schmux workspaces can't see each other. They don't know
what other agents are working on or whether their planned changes
will conflict. This was the coordination problem from the Feb 7-10
multiplayer experience where two developers independently rebuilt
PTY streaming.

### The Tension

PHILOSOPHY.md says: *"The human is always the coordinator."* Exposing
MCP tools to agents would shift that boundary -- agents would have
some autonomous awareness of each other.

This is a deliberate design choice to discuss, not something to
slip in. The question for the team: should agents have read-only
awareness of other sessions (what's running, what files are being
touched), even if the human remains the decision-maker?

### Serge's Skepticism

Serge questions whether coordination is actually needed. His argument:
agents reduce alignment costs (they just re-do work if there's a
conflict, it's cheap). The friction to coordinate may exceed the cost
of occasional merge conflicts.

This is a valid challenge. MCP awareness should be evaluated against
this bar: does it prevent enough wasted work to justify the complexity?
Or is "let them conflict, rebase" good enough?

What Serge IS interested in is **comprehension, not coordination** --
understanding what 46 commits across 5 sessions actually accomplished,
not preventing them from conflicting. He's described a multi-step
analysis: (1) model the repo's architectural layers, (2) analyze agent
conversations against that model for a TLDR, (3) compare TLDRs across
workspaces for semantic overlap. He calls this a "metis channel" --
practical situational awareness, not formal alignment.

This is a different problem than MCP coordination tools solve. It's
closer to the floor manager concept in chaplin, but focused on post-hoc
comprehension rather than real-time intervention.

### What MCP Coordination Would Look Like

Schmux would run an MCP server (stdio transport, injected via
`--mcp-server` flag or agent config) with read-only tools:

| Tool | Purpose |
|------|---------|
| `schmux_list_sessions` | What other agents/humans are working? Returns workspace names, branches, agent targets, status. |
| `schmux_check_conflicts` | Given a list of files, are any being modified in another workspace? Uses git status data schmux already computes. |
| `schmux_get_workspace_info` | Current workspace details (path, branch, repo, environment). |

Note what's NOT here: no `schmux_workspace_intent` (too speculative),
no `schmux_signal_status` (CLI flag injection works fine), no write
operations. Read-only awareness only.

### Why This Might Be Worth It

The data already exists. Schmux already polls `git status` across all
workspaces (`UpdateAllGitStatus`), already tracks which sessions are
active and in which workspaces, already knows which files are dirty
per workspace. Exposing this to agents via MCP is plumbing, not new
intelligence.

An agent that can check "is anyone else touching `src/api/routes.go`?"
before rewriting it would have prevented several of the conflicts
from the multiplayer week.

### Implementation: Separate Binary

The MCP server would be a separate Go binary (`cmd/schmux-mcp/`) that
reads schmux's state file and REST API. This keeps it decoupled:

- Schmux daemon doesn't need to know about MCP
- The MCP binary can evolve independently
- Easy to test (it's just an API client)
- Inject via agent config: `"mcp_servers": [{"command": "schmux-mcp"}]`

---

## How This Relates to Active Work

### Stacked Diffs / Child Workspaces

Serge's proposal: spawning in a workspace creates a child workspace
(new worktree branching from current, not main). This is about git
workflow for local use. It should work identically for remote workspaces
since the worktree mechanism is the same -- the bare clone pool is
local to wherever schmux is running.

The `wrapper_command` config would propagate to child workspaces
automatically (it's per-repo, not per-workspace).

### Workspace Recycling

The recent `CommitsSyncedWithRemote` fix makes disposal smarter for
remote workflows where branches are pushed but `@{u}` points elsewhere.
This directly enables the remote multi-project topology -- workspaces
can be recycled/disposed without false "unpushed commits" warnings.

### Config Isolation (`SCHMUX_HOME`)

Stefano wants this for dev vs production schmux. But it's also critical
for the remote topology: multiple schmux instances on the same machine
(one per team? one per project?) need separate config/state. Same
feature, different motivation.

### Overlay Compounding

Stefano is working on auto-syncing overlays (`.env`, secrets, perms)
across workspaces. This is the secrets side of environment management.
`wrapper_command` is the toolchain side. Both are needed for remote
multi-project setups where each repo has different secrets and different
runtimes.

### Preview Proxy

The workspace preview proxy (auto-detect dev servers, ephemeral reverse
proxy) currently only supports local workspaces. For remote schmux, this
becomes even more valuable -- developers can preview workspace web apps
through the dashboard without knowing which port Vite grabbed on the
remote machine. Extending the proxy to work when the dashboard is
accessed remotely is a natural follow-on.

---

## Composition with Chaplin

Stefano's chaplin proposal (`docs/specs/multiplayer-orchestration.md`)
addresses multi-operator coordination: how multiple people running
separate schmux instances collaborate without merge conflicts or
duplicated effort. It proposes a separate Go service with projects,
activities, users, WebSocket state sync, group chat, and a floor
manager agent.

**These proposals address different layers:**

```
+-----------------------------------------------------+
| Layer 3: Federation / Coordination  (chaplin)       |
|   Projects, activities, group chat, floor manager   |
|   WebSocket sync between schmux instances           |
|   Terminal relay (lazy-pull), activity recordings    |
+-----------------------------------------------------+
| Layer 2: User Identity (gap -- neither addresses)   |
|   Who is this? SSH key -> display name mapping      |
|   Per-user workspace ownership on shared machine    |
|   Lightweight, no OAuth/auth server required        |
+-----------------------------------------------------+
| Layer 1: Workspace Runtime  (this proposal)         |
|   Worktree lifecycle, environment activation        |
|   IDE connection info, ACP facilitation/client      |
|   MCP read-only awareness, wrapper_command          |
|   Single daemon, no coordination service needed     |
+-----------------------------------------------------+
```

### Where They Agree

- No automated agent-to-agent orchestration (human coordinates)
- Git as the source of truth for conflict detection
- Privacy/sharing is opt-in (chaplin: project visibility settings)
- YAGNI (no workflow engines, approval gates, or role hierarchies)

### Where They Tension

| Topic | This proposal | Chaplin |
|-------|---------------|---------|
| Deployment | Single schmux, multiple users on same machine | Multiple schmux instances, coordinated via cloud/LAN service |
| User identity | Implicit (SSH user, or single shared user) | Explicit (OAuth, user profiles, presence) |
| Communication | Dashboard terminal + IDE (direct) | Group chat per project, floor manager agent |
| Coordination | MCP read-only awareness (agents see state) | Floor manager agent mediates via group chat |
| Minimum viable | Works with zero new services | Requires chaplin service running |

### Chaplin's "Encompasses Both" Argument

Chaplin's spec describes two deployment scenarios:

1. **Centralized**: One schmux + chaplin co-located on same machine.
   This maps to the remote multi-project topology.
2. **Federated**: Separate schmux instances (each user's laptop) +
   cloud-hosted chaplin for coordination.

The argument: the coordination protocol is identical in both cases,
just different network topology. If you build chaplin, you get the
centralized case for free.

The gaps for the remote-workspace use case:
- Chaplin still requires running a separate coordination service
  even for the simplest case (one machine, two developers)
- User identity in chaplin is web-first (OAuth), not SSH-first
  (which is how remote users actually arrive)
- The project/activity hierarchy adds friction for lightweight use
- Group chat assumes synchronous coordination; for async work across
  timezones, passive awareness may be more practical
- Chaplin doesn't address environment management, IDE connection,
  or ACP -- those are workspace-layer concerns regardless

**The practical path:** Build Layer 1 (this proposal) without
dependency on chaplin. If/when chaplin arrives, it layers on top.
Layer 2 (user identity) is the missing glue -- whoever builds it
first (schmux or chaplin) defines how users are represented.

---

## What This Does NOT Propose

- **Multi-tenancy.** No auth, permissions, or user isolation. Trusted
  teams only.
- **Replacing the dashboard.** Dashboard remains primary UI. IDE
  integration is additive. ACP 3b would evolve the dashboard, not
  replace it.
- **Building an editor.** Schmux is "chat-centric." For code editing,
  use VSCode Remote, Zed SSH, or your editor of choice. Schmux
  facilitates the connection, not the editing.
- **Dropping terminal observation.** Even with ACP 3b, tmux sessions
  remain for non-ACP agents and as a fallback/debug view.
- **Agent-to-agent orchestration.** MCP tools provide read-only
  awareness. Humans remain coordinators.
- **Container isolation (yet).** Deferred. `wrapper_command` +
  direnv/nix handles the environment correctness problem without
  container overhead. Containers only if someone needs true OS-level
  isolation.

---

## Implementation Phases

### Phase 1: `wrapper_command` (Smallest useful change)

- [ ] Add `wrapper_command` field to `Repo` config struct
- [ ] Template rendering (reuse `RemoteFlavor` template pattern)
- [ ] Prepend to agent spawn command in session manager
- [ ] Document in `docs/cli.md` and `docs/api.md`

No dashboard changes. No new endpoints. Just a config field that wraps
the spawn command. Local workflows: unaffected (field is empty, no
wrapping).

### Phase 2: `editor-info` Endpoint + Dashboard Links

- [ ] Add `ssh_host`/`ssh_user` to network config
- [ ] Implement `GET /api/workspaces/:id/editor-info`
- [ ] Add "Open in..." buttons to workspace cards in dashboard
- [ ] Deep link URI construction for Zed, VS Code, JetBrains

Low-risk. The endpoint is read-only. Deep links are just URI strings.
For local schmux, buttons could link to `file://` paths or be hidden.

### Phase 3a: ACP Socket Facilitation

- [ ] Set `SCHMUX_ACP_SOCKET` env var when spawning agent sessions
- [ ] Include `acp_socket` path in `editor-info` response
- [ ] Document SSH tunnel setup for IDE-to-agent ACP connection

Depends on agent-side ACP socket support. Schmux's part is minimal:
set an env var, expose its value. The IDE and agent do the rest.

### Phase 3b: Schmux as ACP Client (if team wants to go bigger)

- [ ] ACP client library in Go (JSON-RPC 2.0, stdio transport)
- [ ] Alternative session spawn path: ACP subprocess instead of
      tmux-only
- [ ] Dashboard structured view: tool calls, plans, file changes, chat
- [ ] Hybrid routing: ACP for capable agents, tmux for everything else
- [ ] Resolve control-vs-observation tension in PHILOSOPHY.md

This is a significant architectural evolution. Phase 3a is prerequisite
exploration. 3b should only proceed after experience with ACP-capable
agents in schmux via 3a shows the structured data is genuinely more
useful than terminal observation for the dashboard use case.

### Phase 4: MCP Coordination (if team agrees on philosophy shift)

- [ ] `cmd/schmux-mcp/` binary reading state + API
- [ ] `schmux_list_sessions`, `schmux_check_conflicts`,
      `schmux_get_workspace_info` tools
- [ ] Documentation for injecting MCP server into agent config
- [ ] Discussion: does this change PHILOSOPHY.md's "human is
      coordinator"?

### Phase 5: Container Isolation (deferred, only if needed)

- [ ] Distrobox/Docker per-workspace support
- [ ] `container_image` config field per repo
- [ ] Container lifecycle tied to workspace lifecycle
- [ ] IDE attachment (Dev Containers protocol, SSH into container)

---

## Open Questions

### Architecture

1. **What IS schmux becoming?** A headless agent runtime (tmux +
   worktrees + ACP)? A dev workspace platform? An orchestration
   surface? The Zed comparison suggests "headless runtime" is the
   right framing, but the team should align on this before building
   features that pull in different directions.

2. **ACP: facilitation or full client?** Phase 3a (set env var, expose
   socket path) is trivial and low-risk. Phase 3b (schmux as ACP
   client) is a bigger bet that changes what the dashboard is. Should
   we start with 3a to get experience, or does 3b's structured data
   model solve enough pain (NudgeNik fragility, signal marker hacks)
   to justify the investment sooner? Note: 3b shifts schmux from
   observer to controller -- is that the right direction?

3. **Remote schmux on a shared machine: one daemon or many?** One
   schmux instance for all users on the machine? Or one per user with
   `SCHMUX_HOME` isolation? The trust model says one is fine, but
   config isolation (`SCHMUX_HOME`) may be needed regardless for the
   dev-vs-production problem.

### Coordination

4. **Does coordination pass Serge's bar?** Serge asks: why does someone
   feel blocked? If agents can cheaply redo work, maybe merge conflicts
   aren't worth preventing. MCP awareness and chaplin both need to
   justify themselves against "let them conflict and rebase."

5. **Comprehension vs coordination.** Serge's "metis channel" concept
   -- understanding what happened across sessions -- may be more
   valuable than preventing conflicts. Is this a schmux feature, a
   chaplin feature, or a separate tool?

6. **Chaplin: prerequisite, parallel, or deferred?** This proposal is
   designed to work WITHOUT chaplin. If chaplin lands first, it layers
   on top. But Layer 2 (user identity) is a gap in BOTH proposals --
   whoever builds it first defines how users are represented.

### Implementation

7. **`wrapper_command` -- too simple or just right?** Is a string
   template sufficient, or do we need structured environment config
   (detect `.envrc`, choose tier, etc.)? Start simple, add detection
   if we find ourselves repeating patterns.

8. **Should `editor-info` be remote-only or universal?** Deep links to
   `file://` paths work locally too (open in Zed/VS Code from dashboard
   even on localhost). Might be useful for everyone.

### Ecosystem

9. **Zed as frontend.** If team members use Zed, its collaboration
   features (channels, shared projects) overlap with chaplin's group
   chat. Does schmux need its own coordination layer, or does "schmux
   as Zed backend" plus Zed's native collaboration suffice?

10. **ACP socket transport.** Phase 3a depends on agents supporting ACP
    over Unix domain sockets (not just stdio). Claude Code and others
    are moving in this direction but it's not landed. Timeline matters
    for planning.

---

## References

- PHILOSOPHY.md -- "The human is always the coordinator"
- `docs/specs/multiplayer-orchestration.md` -- Chaplin spec (PR #18)
- `internal/config/config.go` -- RemoteFlavor, Repo struct
- `internal/workspace/manager.go` -- Workspace lifecycle
- `internal/session/manager.go` -- Session spawn flow
- ACP spec: https://agentclientprotocol.com/
- MCP: https://modelcontextprotocol.io/
- Zed AI: https://zed.dev/docs/ai/overview
- Zed agent panel: https://zed.dev/docs/ai/agent-panel
- Zed external agents (ACP): https://zed.dev/docs/ai/external-agents
- Zed channels: https://zed.dev/docs/collaboration/channels
- Zed remote development: https://zed.dev/docs/remote-development
- direnv: https://direnv.net/
- distrobox: https://distrobox.it/
- VS Code Remote SSH: https://code.visualstudio.com/docs/remote/ssh
