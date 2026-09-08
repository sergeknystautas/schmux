# NudgeNik

**Problem:** Coding agents and LLMs are inherently powerful _because_ they aren't binary and can operate in ambiguous spaces. But most orchestration tools are attempting to squash that ambiguity rather than recognize that software development is ambiguous — it's messy, requires judgment, and isn't reducible to binary pass/fail metrics.

---

## What NudgeNik Does Today

NudgeNik reads what agents recently did and concludes what they're up to.

### Status Interpretation

NudgeNik summarizes the agent's state into one of:

- **Needs Input**: Agent has a question or needs user guidance
- **Needs Feature Clarification**: Agent needs clarification on requirements
- **Needs Attention**: Agent needs authorization or is stuck
- **Completed**: Finished all work

This is valuable right now for:

- Triage: Know which sessions need your attention first
- Quick assessment: Scan many sessions at a glance
- Focus allocation: Don't waste time on agents that are still working

### Technical Background

NudgeNik uses an LLM to read the English output of coding agents and classify their state.

---

## Direct Agent Signaling

NudgeNik can be augmented with direct agent signaling for cheaper and more reliable status updates. See [Agent Signaling](agent-signaling.md) for full details.

### How They Work Together

| Scenario                 | What Happens                                          |
| ------------------------ | ----------------------------------------------------- |
| Agent supports signaling | Direct signals used; NudgeNik skipped (saves compute) |
| Agent doesn't signal     | NudgeNik analyzes output as before                    |
| No signals for 5+ min    | NudgeNik kicks in as fallback                         |

### API Distinction

Both mechanisms update the same nudge fields for frontend compatibility:

- Direct signals: `source: "agent"` in the API response
- NudgeNik classification: `source: "llm"` in the API response

---

## Frontend Rendering

Both nudge sources (signals and NudgeNik) update the same session fields
(`nudge_state`, `nudge_summary`, `nudge_seq`) that the UI reads. The
preview renders in two places using the same conditional:

- `assets/dashboard/src/components/AppShell.tsx` — sidebar row, second line (`nav-session__row2`)
- `assets/dashboard/src/components/SessionTabs.tsx` — bottom session tab, second line

### Row2 is conditionally mounted

Row2 mounts/unmounts on nudge-state transitions, which would shift the
row height — and every row below it — on each change. Three rules keep
the list stable:

- **Working** — render the spinner inline in row1, not row2. Working
  flips frequently; row2 churn would reflow the sidebar on every
  pause/resume.
- **Idle** — suppress row2 entirely. Idle is a settled state, not an
  attention signal.
- **Focused session** — show row2 unconditionally. A previous revision
  hid row2 for the session the user was viewing ("the user is already
  looking at it"), but the focus-driven mount/unmount shifted the
  entire sidebar on every navigation — the same reflow class the
  Working rule was designed to avoid. Always-render placeholders were
  considered and rejected: a permanent empty gap on every row to avoid
  a rarer, content-driven shift is worse than the focused-session
  state change.

### Ack is separate from display

`nudge_seq` is a sound-ack counter — replayed once on the WebSocket
update that increments it, then acked in localStorage so reloads don't
replay it. It is independent of row2 visibility: bumping `nudge_seq`
does not hide the row. Ack logic lives in
`assets/dashboard/src/contexts/SessionsContext.tsx`;
`SessionDetailPage.tsx` lists it as an effect dep so the ack fires
when the user opens the session.

---

## Where This Is Going

Using an LLM to read the English output of coding agents opens the door for more human-centric agent organization.

Instead of creating strict orchestration that requires very clear goals, we recognize that software development is messy and requires interpretation.

---

## Future Vision

### Evaluate What Agents Are Doing

Did they actually run the tests they claimed? Do they need integration testing? Did they finish the requirements?

### Ask (Almost Rhetorical) Questions

When agents are stuck or looping on a problem, NudgeNik could prompt:

- "This agent has retried the same approach 5 times. Try a different model?"
- "Tests are failing but the agent claims success. Review needed."

### Suggest Next Steps

- "All agents agree on the approach. Ready to merge?"
- "Conflicting solutions across agents. Review diffs before proceeding."
- "This agent made more progress than others. Consider promoting its approach."

### Seek Other Expertise

When progress has stalled:

- Suggest trying a different model to think differently
- Flag that human intervention is needed
- Recommend bringing in a specialist agent

---

## The Future Isn't Binary Orchestration

The future of agent coordination isn't strict state machines and binary pass/fail metrics.

It's **interpretation and judgment**.

NudgeNik represents a shift from mechanical orchestration to intelligent assistance—helping you understand what's happening across many agents, and suggesting where to focus your attention.

---

## References

- [Code Is Cheap Now. Software Isn't.](https://www.chrisgregori.dev/opinion/code-is-cheap-now-software-isnt) — Chris Gregori
- [Your Dev Environment Should Also Not Be Overcomplicated](https://adventurecapital.substack.com/p/your-dev-environment-should-also) — Ben Mathes
- [Clerky's tweets on Claude Code development workflow](https://x.com/bcherny/status/2007179832300581177)
