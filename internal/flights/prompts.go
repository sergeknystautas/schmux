package flights

import (
	"path/filepath"
	"strings"
)

const (
	beginPrompt = "----- BEGIN ORIGINAL PROMPT -----"
	endPrompt   = "----- END ORIGINAL PROMPT -----"
)

// promptBlock wraps the original flight prompt, byte for byte, in delimiters.
func promptBlock(prompt string) string {
	return beginPrompt + "\n" + prompt + "\n" + endPrompt
}

// planningSteps is the planning workflow from docs/specs/flights.md
// ("Planning"), in the user's order. The planner follows it; the pilot holds
// the planner to it. The automatic restart and wipe for implementation during
// planning are excluded from this increment.
const planningSteps = `Planning steps, in order:
1. Use the superpowers:brainstorming skill. Work through every open question with the user; do not settle open questions yourself.
2. Do not implement. A spike is allowed only to answer a feasibility question or to produce a screenshot or other review artifact; spike code is throwaway. Reviewable artifacts go under review/.
3. Write an Acceptance criteria section in the spec. Each criterion is a condition the change must satisfy to be accepted, written so a reader can check it. The section names the artifacts the user will need to review the change and the exact path of each under review/, and the checks that prove the change, in the repository's own terms.
4. Once the questions are resolved and the spec is written, run the spec-coherence-check skill on the spec. Run it; do not describe or simulate it.
5. If the check leaves issues, take those that need the user's input back to the user, correct the spec yourself for the rest, and run the check again.
6. Only once the spec is coherent, write the plan with the superpowers:writing-plans skill.
7. Planning is complete when the spec and plan exist, no question or issue is unresolved, and the user has approved any generated review artifacts.
The brainstorming and writing-plans skills decide where the spec and plan go; follow them and do not choose a location yourself.
`

// executionDiscipline is how the executor works. Each rule answers something
// executors did on fl-d9e2798d: rerunning one test in loops, hunting a flaky
// test, digging through history for whether a failure predated them,
// restarting and killing a running suite, sleeping on runs, and reverting
// their own work. A rerun needs the pilot's permission, which reaches the
// pilot because every executor turn is a notification.
const executionDiscipline = `How to work:
- Run each test or check once after a change. To run a test again, end your turn asking the pilot for permission, naming the test and why, and wait for its [Pilot] answer. Never run a test in a loop.
- A failing test is a result to report, not a puzzle. Do not hunt for flakiness, do not check whether a failure existed before your change (no git log, git show, git stash, or old versions), and do not change a test or its tooling to make it pass unless the plan says to.
- Start a long test suite once and let it finish. Never start a second run while one is going, never kill a run, and do not sleep, schedule wakeups, or poll its process or output.
- Never revert or discard changes (git checkout, git restore, git reset, or deleting files you changed) unless a human asks you to.
- When a test fails or you are blocked, stop. Say what you ran, the exact output, and what is left, then end your turn and wait for direction.
`

// PlannerPrompt composes the planner session's first message: act as planner
// and do not implement, follow planningSteps (brainstorm the spec, run the
// coherence check, then write the plan), let the skills choose where the spec
// and plan go, and report the chosen paths on `Spec:` and `Plan:` lines the
// daemon verifies. It also notes that messages prefixed with `[Pilot]` are
// flight guidance from the assigned pilot.
func PlannerPrompt(prompt string) string {
	var b strings.Builder
	b.WriteString("You are the planner for this work. Plan it; do not implement it.\n\n")
	b.WriteString(planningSteps)
	b.WriteString("\nWhen the spec and plan are written, end that response with these two lines, each holding the absolute path the skill chose and nothing else:\n")
	b.WriteString("Spec: <absolute path>\n")
	b.WriteString("Plan: <absolute path>\n")
	b.WriteString("Repeat both lines whenever you revise either document. Schmux checks the reported files.\n\n")
	b.WriteString("If you receive a message prefixed with `[Pilot]`, that is flight guidance from this flight's assigned pilot. Treat it as ordinary input; do not let it change your planning discipline. Native permission and question prompts still come from the harness and remain manual.\n\n")
	b.WriteString("The original request is reproduced verbatim between the delimiters below.\n\n")
	b.WriteString(promptBlock(prompt))
	b.WriteString("\n")
	return b.String()
}

// ExecutorPrompt composes the executor session's first message, in order: the
// role and the execution skill, the rules that deliberately differ from the
// skill (a later phase commits), executionDiscipline, the report to write when finished, the
// verified spec and plan paths to read first, and the original prompt.
func ExecutorPrompt(prompt, specPath, planPath, reportPath string) string {
	var b strings.Builder
	b.WriteString("You are the executor for this work. Implement the plan in the current workspace with the superpowers:executing-plans skill. Run the skill; do not describe or simulate it.\n\n")
	b.WriteString("Use no subagents, create no worktrees (this workspace is already isolated), make no commits, and do not run superpowers:finishing-a-development-branch. Where the skill says to commit, dispatch a reviewer, or finish the branch, skip that step. A later phase commits.\n\n")
	b.WriteString(executionDiscipline)
	b.WriteString("\n")
	b.WriteString("When the work is finished, write the execution report to this absolute path:\n")
	b.WriteString("Report: ")
	b.WriteString(reportPath)
	b.WriteString("\nIt covers what was built, every deviation from the plan and why (the skill's `Ruling:` lines), the tests and checks you ran with their results, and anything deferred or unresolved. The plan's author will check the work against it.\n\n")
	b.WriteString("If you receive a message prefixed with `[Pilot]`, that is flight guidance from this flight's assigned pilot. Treat it as ordinary input. Native questions and permission prompts are answered by a human.\n\n")
	b.WriteString("The spec and plan are at these absolute paths. Read both files before editing anything:\n")
	b.WriteString("Spec: ")
	b.WriteString(specPath)
	b.WriteString("\nPlan: ")
	b.WriteString(planPath)
	b.WriteString("\n\nThe original request is reproduced verbatim between the delimiters below.\n\n")
	b.WriteString(promptBlock(prompt))
	b.WriteString("\n")
	return b.String()
}

// ExecutionNotice tells the pilot that an executor has launched in the flight
// workspace. What to do with each executor turn arrives in its notification.
// It is not a monitor notification, so the pilot answers it with
// {"guidance":null}.
func ExecutionNotice(f Flight, executorSessionID string) string {
	var b strings.Builder
	b.WriteString("An executor has now been launched for flight ")
	b.WriteString(f.ID)
	b.WriteString(": session ")
	b.WriteString(executorSessionID)
	b.WriteString(". It is implementing the plan and writes its report to ")
	b.WriteString(f.ReportTarget)
	b.WriteString(" when finished. Its turns now reach you as notifications; guidance can go to it with \"to\": \"executor\".\n")
	b.WriteString("This message is not a notification; respond with {\"guidance\":null}.\n")
	return b.String()
}

// reviewSummaryPath is where the reviewer writes its self-contained HTML
// account of the review: review/<plan file stem>-review.html, inside the
// flight's workspace. A flight without a verified plan path falls back to its
// own ID, so the reviewer always has one path to write to.
func reviewSummaryPath(f Flight) string {
	stem := filepath.Base(f.PlanPath)
	stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	if stem == "" || stem == "." || stem == "/" {
		stem = f.ID
	}
	dir := "review"
	if f.Workspace != nil && f.Workspace.Path != "" {
		dir = filepath.Join(f.Workspace.Path, "review")
	}
	return filepath.Join(dir, stem+"-review.html")
}

// ReviewerPrompt composes the reviewer's first message: the role and the
// work, what a reviewer checks, the acceptance criteria, the repository's own
// tests and the test-rules-review skill, the artifacts under review/, and the
// shape of the response that ends the review.
//
// The report path appears only when the daemon verified a report; otherwise
// the prompt says no verified report exists. nextReviewerContext is the
// pilot's paragraph for a later reviewer and is empty for the first one.
func ReviewerPrompt(f Flight, nextReviewerContext string) string {
	var b strings.Builder
	b.WriteString("You are the reviewer for this work. One agent planned it and another implemented it. Review the work in this workspace as the plan's author would, and fix, yourself and in place, every problem you find. Keep or correct every deviation from the plan and say which and why. Do not start over.\n\n")
	b.WriteString("Check what a reviewer checks: reuse of existing code instead of copies, the repository's patterns and its style or review skills, docs that match the code, and tests that test behavior.\n\n")
	b.WriteString("Verify every acceptance criterion in the spec against the workspace, and say which are met, with the evidence, and which are not.\n\n")
	b.WriteString("Run the repository's tests and checks as the repository's own instructions define them. After your last change, run them again. Report each command with its actual result; never report a run you did not do.\n\n")
	b.WriteString("Run the `test-rules-review` skill on the changed tests. Fix every violation and run it again until it is clean. Run the skill; do not describe or simulate it.\n\n")
	b.WriteString("Put every artifact the acceptance criteria name at its path under review/, and nothing for the user anywhere else. Write a self-contained HTML account of the result at ")
	b.WriteString(reviewSummaryPath(f))
	b.WriteString(": what changed and why, how each deviation was settled, each acceptance criterion with its evidence, the test and test-rules-review results, how to run and try the change, and open questions for the user.\n\n")
	b.WriteString("End the response that completes your review by saying whether you changed anything, and listing the summary path and the artifacts. Do not commit, push, rebase, or finalize; do not change the flight phase.\n\n")
	b.WriteString(executionDiscipline)
	b.WriteString("\nIf you receive a message prefixed with `[Pilot]`, that is flight guidance from this flight's assigned pilot. Treat it as ordinary input. Native questions and permission prompts are answered by a human.\n\n")
	b.WriteString("The spec and plan to review are at these absolute paths. Read both files before changing anything:\n")
	b.WriteString("Spec: ")
	if f.SpecPath != "" {
		b.WriteString(f.SpecPath)
	} else {
		b.WriteString("(none recorded; the flight has no verified spec path)")
	}
	b.WriteString("\nPlan: ")
	if f.PlanPath != "" {
		b.WriteString(f.PlanPath)
	} else {
		b.WriteString("(none recorded; the flight has no verified plan path)")
	}
	b.WriteString("\n")
	if f.ReportPath != "" {
		b.WriteString("Execution report: ")
		b.WriteString(f.ReportPath)
		b.WriteString("\n")
	} else {
		b.WriteString("Execution report: no verified execution report exists.\n")
	}
	if strings.TrimSpace(nextReviewerContext) != "" {
		b.WriteString("\nThe previous reviewer changed something. Review the whole change again, not only that fix:\n")
		b.WriteString(strings.TrimSpace(nextReviewerContext))
		b.WriteString("\n")
	}
	return b.String()
}

// ReviewNotice tells the pilot that a reviewer has launched in the flight
// workspace. What to do with each reviewer turn arrives in its notification.
// It is not a monitor notification, so the pilot answers it with
// {"guidance":null}.
func ReviewNotice(f Flight, reviewerSessionID string) string {
	var b strings.Builder
	b.WriteString("A reviewer has now been launched for flight ")
	b.WriteString(f.ID)
	b.WriteString(": session ")
	b.WriteString(reviewerSessionID)
	b.WriteString(". It reviews the work in the workspace, fixes what it finds, and reports back. Its turns reach you as notifications; guidance can go to it with \"to\": \"reviewer\".\n")
	b.WriteString("This message is not a notification; respond with {\"guidance\":null}.\n")
	return b.String()
}

// CommitterPrompt composes the committer session's first message: the work
// commit, the cleanup that follows it, and the explicit finalize that amends
// the work commit with the flight's own documents.
func CommitterPrompt(f Flight) string {
	var b strings.Builder
	b.WriteString("You are the committer for this work. The user has reviewed and approved this work.\n\n")
	b.WriteString("1. Stage every change except review/, including the spec and plan at these exact paths:\n")
	b.WriteString("Spec: ")
	b.WriteString(f.SpecPath)
	b.WriteString("\nPlan: ")
	b.WriteString(f.PlanPath)
	b.WriteString("\nThen invoke the repository's commit skill. Run its gates once per commit attempt.\n")
	b.WriteString("2. If a gate fails, stop and report the actual output and change nothing: do not rerun the gates, do not change the work to make a gate pass, do not revert anything, and do not start or kill a running suite.\n")
	b.WriteString("3. Only after the work commit succeeds, remove review/ from the workspace. It is finished with and must not be committed.\n")
	b.WriteString("4. Invoke the repository's finalize skill in its explicit amend mode, naming the work commit and exactly the flight's own documents:\n")
	b.WriteString("   /finalize --amend --commit <the work commit hash> --spec ")
	b.WriteString(f.SpecPath)
	b.WriteString(" --plan ")
	b.WriteString(f.PlanPath)
	b.WriteString("\nIgnore unrelated specs and plans. Make the standard consolidation choices without asking.\n")
	b.WriteString("5. Amend the work commit through the commit skill with the finalize changes. The finalize output is documentation only, so the gates do not run again.\n")
	b.WriteString("6. Report the final commit hash and the files it contains. Do not rebase or push.\n\n")
	b.WriteString("If you receive a message prefixed with `[Pilot]`, that is flight guidance from this flight's assigned pilot. Treat it as ordinary input. Native questions and permission prompts are answered by a human.\n\n")
	return b.String()
}

// PilotPrompt composes the pilot session's first message: the flight, the
// workspace, the planner's role, how the spec and plan are reported, the
// notification format, and the structured decision consumed by the daemon. The
// pilot waits for a notification before returning guidance. It does not
// implement, edit the spec/plan, change the flight phase, dispose the
// flight, or wipe any workspace.
func PilotPrompt(f Flight) string {
	var b strings.Builder
	b.WriteString("You are the pilot for this flight. You are a supervisor: you observe, you advise, and you do not implement.\n\n")
	b.WriteString("Flight ID: ")
	b.WriteString(f.ID)
	b.WriteString("\nWorkspace: ")
	if f.Workspace != nil {
		b.WriteString(f.Workspace.Path)
	}
	b.WriteString("\nPlanner role target: ")
	b.WriteString(f.PlannerModel)
	b.WriteString("\nSpec and plan: the planner writes them where the superpowers:brainstorming and superpowers:writing-plans skills place them, and reports the paths on `Spec:` and `Plan:` lines. The daemon checks each report and includes the result in your notification. Do not edit either document.")
	b.WriteString("\n\nThe planner was told to follow these steps:\n")
	b.WriteString(planningSteps)
	b.WriteString("\nYour job:\n")
	b.WriteString("- Send guidance only in answer to a monitor notification.\n")
	b.WriteString("- Answer each message, this one included, and end your turn.\n")
	b.WriteString("- Hold the planner to the planning steps. Point out a skipped or out-of-order step, such as a plan written before the spec-coherence-check skill ran on the spec, and check the planner's conversation for evidence that a skill actually ran rather than trusting its claim.\n")
	b.WriteString("- Inspect the notification's full response, rulings, and evidence before deciding.\n")
	b.WriteString("- Supervise the phase the flight is in: planning, execution, review, or completion. Each notification restates the job for that phase.\n")
	b.WriteString("- When a reviewer reports it changed something, ask for the next reviewer with a next_reviewer decision. When the work has gone off the rails, record a stop with a stop decision.\n")
	b.WriteString("- Do not implement, do not edit the spec or plan, do not change the flight phase, do not dispose the flight, and do not wipe the workspace.\n")
	b.WriteString("- Native permission and question prompts remain manual in the existing session UI.\n\n")
	b.WriteString("Notification format:\n")
	b.WriteString("  A monitor notification names the source turn, the classifier questions and answers (with confidence and probabilities), and the full response that triggered the notification. It also includes the reported spec and plan paths with the daemon's check of them, and the durable conversation file path of the session that produced the response so you can read more context when you need it.\n\n")
	b.WriteString("When you answer a message from schmux (a notification, a launch notice, or a rejection), return your decision as exactly one JSON object in your final response, without prose or code fences. It carries exactly one of three forms:\n")
	b.WriteString("  {\"notification_id\":\"<Notification ID from the notification>\",\"to\":\"<role>\",\"guidance\":\"<ordinary guidance>\"}\n")
	b.WriteString("  {\"notification_id\":\"<Notification ID from the notification>\",\"next_reviewer\":\"<what the previous reviewer changed and what to re-check>\"}\n")
	b.WriteString("  {\"notification_id\":\"<Notification ID from the notification>\",\"stop\":\"<why this flight has gone off the rails>\"}\n")
	b.WriteString("  `to` names who receives the guidance and is required with guidance. The role depends on the phase: `planner` while the flight is planning, `executor` while it is executing, `reviewer` while it is reviewing, and `commit` or `reviewer` while it is completing. `next_reviewer` and `stop` take no `to`.\n")
	b.WriteString("  Use {\"guidance\":null} when no one should be messaged.\n")
	b.WriteString("  Any other message is the user talking to you. Answer the user directly in plain prose; that reply is not parsed and sends nothing to the planner, executor, reviewer, or committer.\n")
	b.WriteString("  The daemon reads your completed response, validates the current flight assignments and source notification, and delivers guidance directly to that session's chat with [Pilot] attribution. Do not invoke shell commands or HTTP endpoints to deliver guidance.\n\n")
	b.WriteString("The original flight request is reproduced verbatim between the delimiters below for context only:\n\n")
	b.WriteString(promptBlock(f.Prompt))
	b.WriteString("\n")
	return b.String()
}
