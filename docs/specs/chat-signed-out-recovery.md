# Chat Signed-Out Recovery — Requirements

Status: authoritative and self-contained. An implementer with no other
context builds from this document alone.

## Problem

The chat harnesses (claude, codex) authenticate with a first-party login.
When that login is lost — expired, revoked, or logged out elsewhere — the
session looks healthy until the user sends a message and the harness
rejects it. The user needs to be told, in the chat window itself, that the
session cannot authenticate, be prevented from typing into a dead session,
and be given a one-click path to restore the login. All of it must track
real login state live, across sign-out/sign-in cycles, refocuses, and
daemon restarts.

## Concept

`signed_out` is a persisted boolean per chat session in `state.json`. It is
set by the session's own failure, corrected by the harness's status tool,
and displayed by the chat window. One field drives everything: banner,
composer lock, and tab/sidebar badge. Changes go out through the existing
session broadcast so every open page updates together. Persisted state
survives daemon restarts; nothing is rebuilt on load. Nothing in this
feature reads or writes chat history.

Scope: local chat sessions whose target does not route the harness to a
non-first-party endpoint (no `ANTHROPIC_BASE_URL` / endpoint override) —
those never use this login. A session is in scope by resolving its
persisted target at the moment a rule needs the answer; if the target can't
be resolved, the session is in scope (fail toward showing recovery). Remote
chat sessions are out (their login lives on another host). There is no
eligibility field on the session record and nothing is stamped at spawn.

## State rules

**Set — the session's own failure.** When a chat session's turn ends in
error whose text matches a sign-out statement of its harness (claude and
codex each have a small, explicit matcher), set `signed_out` on that
session immediately, persist, broadcast. This is the primary signal: it is
per-session, needs no spawn-time eligibility field, arrives exactly when
the session died, and works for every session regardless of when or how it
was spawned. The set resolves the Concept's scope when it fires — routed
and remote sessions are never set, so no flag is ever left somewhere the
tool's clear cannot reach; an unresolvable target is in scope. The matcher
sees the live turn error only; daemon restart replays no history to derive
the flag. Non-matching errors (usage limits, tool failures, anything else)
set nothing.

**Correct and clear — the status tool.** `claude auth status --json`
(logged in is `"loggedIn": true`) and `codex login status` (logged in
contains `Logged in using ChatGPT`) are the authority on whether the login
works. A run applies to every in-scope chat session of that protocol:

- Logged in → clear `signed_out` on all of them.
- Logged out → set on all of them.
- Timeout or unparseable → no change, log raw output.

Logged-out output shapes have not been observed in the wild; the parser
must treat `loggedIn: false` as logged out and anything unparseable as
no-answer — never guess. Login state is HOME-global, so one run answers
for the protocol. At most one check per protocol in flight; a trigger
arriving during a run is covered by it.

**Tool triggers — exactly two, event-driven.** No interval, no
daemon-startup check.

1. **Chat page focus**: page load, session-tab switch, window refocus,
   visibility change — any activation of the chat page asks the daemon to
   run the check for the session's protocol, unconditionally,
   healthy-looking or bannered alike. A remote session's page never asks
   (its login lives on another host). This is also how "I signed back in"
   gets answered.
2. **Failed turn**: any chat turn ending in error runs the check for that
   session's protocol. The error is a trigger, never a decider — this is
   what retracts a wrongly-set flag (e.g. a usage-limit error whose
   statement matcher fired) within seconds.

## Recovery UX

While `signed_out` is set:

- Warning banner above the composer:
  - claude: "Claude is signed out — messages won't reach it until you sign
    in again."
  - codex: "Codex is signed out. After signing in, restart this session."
- Composer fully disabled — text input, attach, send — with a placeholder
  naming the reason.
- "Signed out" badge on the session tab and in the app sidebar's session
  list.

All clear when the field clears.

What the field tracks is the HOME login, not the session's ability to
deliver. A codex runtime that failed its launch-time account check never
becomes addressable again — the check runs once — so until the session is
restarted, a sent message is accepted, held by the runtime, and delivered
when a restart resumes the conversation: not lost, but not answered.
Whether a codex process that signed out mid-session recovers after
re-login without a restart is unobserved; the banner copy instructs
restart either way.

**Sign-in.** `POST /api/sessions/{sessionID}/reauth` spawns a terminal
session in the chat session's workspace running the harness's real login
flow — `claude auth logout || true; claude /login` for claude,
`codex login` for codex — and the page navigates to it. After signing in, the
user returns to the chat; the focus check clears the flag. Claude uses the
REPL's `/login` dialog, not the standalone `claude auth login` subcommand:
as of Claude Code 2.1.270 that prompt reads the pasted code without echoing
it and exits on a bad code, which reads as a dead terminal.

## Endpoints

- `POST /api/sessions/{sessionID}/reauth` → the spawned session (existing
  `SessionResult` shape); 404 unknown, 400 non-chat, 409 remote.
- `POST /api/sessions/{sessionID}/auth-check` → runs the check for the
  session's protocol; response body unused, state changes arrive via the
  session broadcast. Same guards as reauth: 404 unknown, 400 non-chat,
  409 remote.

Both documented in `docs/api.md` (CI-enforced).

## Design constraints

- No eligibility field persisted at spawn; no predicate on environment
  contents. Env is polluted by design: provider secrets ride in every
  target resolution (a claude spawn command carries `oauth_token=…` from
  the stored anthropic provider secret), so any env-based test is false
  for every session on a configured machine.
- No credentials-file inspection.
- Harness output may set state only through the narrow sign-out matchers
  above; everything else about harness output is a trigger at most.
- No interval, no daemon-startup check.
- The existing nudge/status element in the session UI is untouched.

## Acceptance scenarios

The definition of "works," performed on a machine with provider secrets
configured. Each is verified before the work is called done.

1. With an `anthropic` provider secret stored, sign out of claude
   externally, send a message in a claude chat session: the turn's failure
   sets the banner and locks the composer immediately.
2. Click the sign-in link: the login terminal session spawns in the chat
   session's workspace and the page navigates to it.
3. Complete login, return to the chat, refocus: banner and lock clear via
   the focus check, and the next message gets a reply.
4. Repeat 1–3 for codex, noting which mechanism set the banner — a
   mid-session codex sign-out may error the turn (matcher) or silently
   hold the message (banner then arrives on the next focus check);
   unobserved, so record which. Then, after re-login and before restart,
   a sent message is held; restarting the session delivers it in the
   resumed conversation.
5. With the banner up, restart the daemon and reload: the banner persists;
   refocus after restoring login clears it.
6. Sessions spawned before this feature shipped (no new persisted fields)
   behave identically in 1–5.
7. A usage-limit error does not leave a standing banner (the failed-turn
   tool check retracts any provisional flag).
8. A session routed through `ANTHROPIC_BASE_URL` never shows the banner.
9. Remote chat sessions never show it.
10. A chat page open and idle produces no periodic requests — beyond the
    two resident WebSockets (chat I/O and dashboard) nothing fires;
    checks run on focus/visibility only.
11. The existing session status/nudge element behaves exactly as before.

## Testing requirements

- Statement matcher unit tests per protocol: known sign-out phrasings set;
  usage-limit and unrelated error texts don't. Matchers are explicit
  string lists, not fuzzy classification.
- Tool-checker unit tests with stubbed binaries: logged out, logged in,
  timeout, unparseable, per-protocol command.
- Scope tests: routed targets never flagged; remote never; non-chat never;
  **a config with provider secrets present still flags a first-party
  claude session**.
- Persistence: flag set, state reloaded from disk, flag still set.
- Trigger tests: auth-check endpoint runs the checker; a failed-turn error
  record runs it; a set flag with the tool reporting logged-in clears.
- Frontend tests: banner presence and per-protocol copy; composer
  text/attach/send disabled with reason placeholder; tab and app-sidebar
  badges; all clear when the field clears; focus trigger fires on
  activation and refocus without requiring a banner.
- The acceptance scenarios are actually run and their outcomes reported;
  unit-green alone is not completion evidence.

## Project conventions binding on the implementer

- Types are generated (`go run ./cmd/gen-types`), never hand-edited.
- Dashboard builds only via `go run ./cmd/build-dashboard`; frontend tests
  only via `./test.sh --quick`; completion requires full `./test.sh`,
  `./badcode.sh`, `./format.sh`.
- `docs/api.md` updated for both endpoints; dashboard UI follows
  `docs/dashboard-style-guide.md`.

## Appendix: verified tool behavior

Observed on the target machine, 2026-09-12 (logged-in case only):

```
$ claude auth status --json ; echo $?
{"loggedIn": true, "authMethod": "oauth_token", "apiProvider": "firstParty", ...}
0

$ codex login status ; echo $?
Logged in using ChatGPT
0
```
