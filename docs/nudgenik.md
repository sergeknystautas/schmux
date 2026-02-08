# NudgeNik

**Problem:** Coding agents and LLMs are inherently powerful *because* they aren't binary and can operate in ambiguous spaces. But most orchestration tools are attempting to squash that ambiguity rather than recognize that software development is ambiguous — it's messy, requires judgment, and isn't reducible to binary pass/fail metrics.

---

## What NudgeNik Does Today

NudgeNik reads what agents recently did and concludes what they're up to.

### Status Interpretation

NudgeNik summarizes the agent's state into one of:

- **Blocked**: Needs permission to run a command or approve a change
- **Waiting**: Has a question or needs user input
- **Working**: Actively making progress
- **Done**: Completed all work

This is valuable right now for:
- Triage: Know which sessions need your attention first
- Quick assessment: Scan many sessions at a glance
- Focus allocation: Don't waste time on agents that are still working

### Technical Background

NudgeNik uses an LLM to read the English output of coding agents and classify their state. See `docs/dev/cursor-positions.md` for technical notes on terminal behavior and analysis.

---

## Direct Agent Signaling

NudgeNik can be augmented with direct agent signaling for cheaper and more reliable status updates. See [Agent Signaling](agent-signaling.md) for full details.

### How They Work Together

| Scenario | What Happens |
|----------|--------------|
| Agent supports signaling | Direct signals used; NudgeNik skipped (saves compute) |
| Agent doesn't signal | NudgeNik analyzes output as before |
| No signals for 5+ min | NudgeNik kicks in as fallback |

### API Distinction

Both mechanisms update the same nudge fields for frontend compatibility:

- Direct signals: `source: "agent"` in the API response
- NudgeNik classification: `source: "llm"` in the API response

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
