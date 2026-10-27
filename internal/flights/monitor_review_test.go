package flights

import (
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// The daemon's review evidence names the skill the reviewer's session
// actually used, and never claims more than the session's own records show.
func TestTestRulesReviewEvidence(t *testing.T) {
	turn := func(session string, skills ...SkillUse) MonitoredTurn {
		return MonitoredTurn{Turn: PlannerTurn{SessionID: session, Skills: skills}}
	}
	invoked := SkillUse{Skill: "superpowers:test-rules-review", Seq: 21, Evidence: "invoked"}
	read := SkillUse{Skill: "test-rules-review", Seq: 54, Evidence: "read"}
	other := SkillUse{Skill: "superpowers:executing-plans", Seq: 3, Evidence: "invoked"}
	for _, tc := range []struct {
		name  string
		turns []MonitoredTurn
		want  string
	}{
		{"claude invocation", []MonitoredTurn{turn("reviewer", invoked)}, "test-rules-review invoked at record 21"},
		{"codex file read", []MonitoredTurn{turn("reviewer", read)}, "test-rules-review skill file read at record 54"},
		{"other skill only", []MonitoredTurn{turn("reviewer", other)}, "test-rules-review not found"},
		{"no skills", []MonitoredTurn{turn("reviewer")}, "test-rules-review not found"},
		{"session outside the reviewer", []MonitoredTurn{turn("planner", invoked)}, "test-rules-review not found"},
		{"earliest wins", []MonitoredTurn{
			turn("reviewer", SkillUse{Skill: "test-rules-review", Seq: 90, Evidence: "invoked"}),
			turn("reviewer", SkillUse{Skill: "test-rules-review", Seq: 40, Evidence: "invoked"}),
		}, "test-rules-review invoked at record 40"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := testRulesReviewEvidence(tc.turns, map[string]bool{"reviewer": true}); got != tc.want {
				t.Errorf("evidence = %q, want %q", got, tc.want)
			}
		})
	}
}

// A completed reviewer turn carries the daemon's skill evidence.
func TestRecordReviewCheckRetainsSkillEvidence(t *testing.T) {
	e := newEnv(t)
	f := review(t, e)
	reviewer := f.sessionFor(RoleReviewer).SessionID
	skill := SkillUse{Skill: "superpowers:test-rules-review", Seq: 12, Evidence: "invoked"}

	id, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: reviewer, StartSeq: 10, EndSeq: 20, Text: "Reviewed.", Outcome: "completed", Skills: []SkillUse{skill}})
	if err != nil {
		t.Fatal(err)
	}
	retained, _ := e.m.findMonitoredTurn(f.ID, id)
	e.m.recordReviewCheck(bg, f.ID, retained)

	got, _ := e.m.findMonitoredTurn(f.ID, id)
	if got.Review == nil || got.Review.Skill != "test-rules-review invoked at record 12" {
		t.Fatalf("review check = %+v, want the invoked evidence", got.Review)
	}
}

// Every reviewer turn reaches the pilot, with the response, the
// classification, and the skill evidence.
func TestMonitorNotifiesEveryReviewerTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "key")
	e := newEnv(t)
	f := review(t, e)
	pilot, reviewer := f.sessionFor(RolePilot).SessionID, f.sessionFor(RoleReviewer).SessionID
	pilotRT, _ := monitorRuntime(t, pilot)
	reviewerRT, _ := monitorRuntime(t, reviewer,
		`{"type":"assistant","uuid":"u1","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"superpowers:test-rules-review"}},{"type":"text","text":"The review is done; I changed nothing."}]}}`,
		`{"type":"result","subtype":"success"}`)
	for id, rt := range map[string]*chat.Runtime{pilot: pilotRT, reviewer: reviewerRT} {
		e.fk.chats.setRuntime(id, rt)
		e.fk.chats.set(id, true)
	}
	routineJev(t)
	events := monitorEvents(e.m, f.ID)
	startTestMonitor(t, e.m)

	delivered := awaitMonitor(t, events, "reviewer notification delivered to the pilot", func(f Flight) bool {
		n := notificationsOf(f)
		return len(n) == 1 && handedToPilot(n[0])
	})

	text := notificationsOf(delivered)[0].Text
	for _, want := range []string{
		"reviewer " + reviewer,
		"Skill check: test-rules-review invoked at record 1",
		"review_state: finished",
		"tests: run_passed",
		"test_rules_review: compliant",
		"changed_work: unchanged",
		"artifacts_named: named",
		"Send guidance to the reviewer",
		"ask for the next reviewer",
		"record a stop",
		"The review is done; I changed nothing.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("notification lacks %q:\n%s", want, text)
		}
	}
	for _, turn := range delivered.Monitor.Turns {
		if turn.Turn.SessionID == reviewer && turn.Review == nil {
			t.Errorf("reviewer turn has no review check: %+v", turn)
		}
	}
}

// A failed reviewer turn reaches the pilot with the harness outcome, without
// a classification.
func TestMonitorNotifiesFailedAndInterruptedTurns(t *testing.T) {
	for _, outcome := range []string{"failed", "interrupted"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			saveMonitorKey(t, "key")
			e := newEnv(t)
			f := review(t, e)
			pilot, reviewer := f.sessionFor(RolePilot).SessionID, f.sessionFor(RoleReviewer).SessionID
			pilotRT, _ := monitorRuntime(t, pilot)
			e.fk.chats.setRuntime(pilot, pilotRT)
			if _, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{
				SessionID: reviewer, StartSeq: 1, EndSeq: 2, Text: "Partial work.",
				Outcome: outcome, Error: "the harness stopped the turn",
			}); err != nil {
				t.Fatal(err)
			}

			e.m.processMonitorWork(bg, f.ID)

			got, _ := e.m.Get(f.ID)
			notifications := notificationsOf(got)
			if len(notifications) != 1 {
				t.Fatalf("notifications = %+v, want one for the %s turn", notifications, outcome)
			}
			for _, want := range []string{"outcome " + outcome, "Harness error: the harness stopped the turn", "Partial work."} {
				if !strings.Contains(notifications[0].Text, want) {
					t.Errorf("notification lacks %q:\n%s", want, notifications[0].Text)
				}
			}
		})
	}
}

// Every committer turn reaches the pilot, with the response and the pilot's
// job for routing a gate failure into the reviewer loop.
func TestMonitorNotifiesEveryCommitterTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "key")
	e := newEnv(t)
	f := complete(t, e)
	pilot, committer := f.sessionFor(RolePilot).SessionID, f.sessionFor(RoleCommit).SessionID
	pilotRT, _ := monitorRuntime(t, pilot)
	committerRT, _ := monitorRuntime(t, committer,
		`{"type":"assistant","uuid":"u1","message":{"content":[{"type":"text","text":"The commit gate failed."}]}}`,
		`{"type":"result","subtype":"success"}`)
	for id, rt := range map[string]*chat.Runtime{pilot: pilotRT, committer: committerRT} {
		e.fk.chats.setRuntime(id, rt)
		e.fk.chats.set(id, true)
	}
	routineJev(t)
	events := monitorEvents(e.m, f.ID)
	startTestMonitor(t, e.m)

	delivered := awaitMonitor(t, events, "committer notification delivered to the pilot", func(f Flight) bool {
		n := notificationsOf(f)
		return len(n) == 1 && handedToPilot(n[0])
	})

	text := notificationsOf(delivered)[0].Text
	for _, want := range []string{
		"commit " + committer,
		"completion_state: working",
		"route the failure to a fresh reviewer",
		"next_reviewer",
		"The commit gate failed.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("notification lacks %q:\n%s", want, text)
		}
	}
}

// The pilot's job for each supervised role arrives with the turn.
func TestPilotJobForRole(t *testing.T) {
	for name, tc := range map[string]struct {
		role SessionRole
		want string
	}{
		"reviewer": {RoleReviewer, "ask for the next reviewer"},
		"commit":   {RoleCommit, "next_reviewer"},
		"executor": {RoleExecutor, `"to": "executor"`},
		"planner":  {RolePlanner, "Check any claimed artifact"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pilotJobForRole(tc.role); !strings.Contains(got, tc.want) {
				t.Errorf("job = %q, want containing %q", got, tc.want)
			}
		})
	}
}
