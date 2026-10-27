package flights

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"

	"gopkg.in/yaml.v3"
)

// The classifier definitions are part of schmux. A file left in ~/.schmux by
// an earlier build is never read, and the readiness check asks only for the
// TypeSafe key.
func TestMonitorUsesSchmuxDefinitionsNotAHomeFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "ignored-environment-key")
	if err := os.MkdirAll(schmuxdir.Get(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(schmuxdir.Get(), "flight-monitor.yaml"), []byte("model: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := MonitorReadinessError(); !strings.Contains(got, "Settings > Flights > Pilot Supervision") {
		t.Fatalf("missing-key readiness = %q", got)
	}
	saveMonitorKey(t, "test-secret-never-returned")
	if got := MonitorReadinessError(); got != "" {
		t.Fatalf("readiness with a key = %q, want ready", got)
	}
	m := New(Dependencies{}, discard())
	if !bytes.Equal(m.monitor.definitions, monitorDefinitionsYAML) {
		t.Fatalf("monitor definitions are not schmux's own flight-monitor.yaml")
	}
}

func TestParseMonitorConfig_RejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"unknown field", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        extra: true\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n", "field extra not found"},
		{"duplicate classifier id", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - {id: a, question: q, choices: [{id: x, description: x}, {id: y, description: y}], notify_on: [x], min_confidence: 0.5}\n      - {id: a, question: q2, choices: [{id: x, description: x}, {id: y, description: y}], notify_on: [x], min_confidence: 0.5}\n", "duplicate"},
		{"notify_on not a choice", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [nope]\n        min_confidence: 0.5\n", "notify_on"},
		{"threshold out of range", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 1.5\n", "min_confidence"},
		{"negative threshold", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: -0.1\n", "min_confidence"},
		{"empty model", "model: ''\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n", "model"},
		{"single choice", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        choices:\n          - {id: x, description: x}\n        notify_on: [x]\n        min_confidence: 0.5\n", "two choices"},
		{"extra yaml document", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        choices: [{id: x, description: x}, {id: y, description: y}]\n        notify_on: [x]\n        min_confidence: 0.5\n---\nmodel: should-not-be-here\n", "extra document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMonitorConfig([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseMonitorConfig() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// testExecutorRoleYAML completes a planner-only fixture, which validation
// rejects without the executor, reviewer, and commit roles.
const testExecutorRoleYAML = "  executor:\n    classifiers:\n      - id: e\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n" +
	"  reviewer:\n    classifiers:\n      - id: r\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: []\n        min_confidence: 0\n" +
	"  commit:\n    classifiers:\n      - id: c\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: []\n        min_confidence: 0\n"

func TestParseMonitorConfig_PreservesOrder(t *testing.T) {
	for _, tc := range []struct {
		yaml string
		want string
	}{
		{"model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q1\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n      - id: b\n        question: q2\n        choices:\n          - {id: yes, description: yes}\n          - {id: no, description: no}\n        notify_on: [yes]\n        min_confidence: 0.5\n", "a,b"},
		{"model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: second\n        question: q\n        choices:\n          - {id: yes, description: yes}\n          - {id: no, description: no}\n        notify_on: [yes]\n        min_confidence: 0.5\n      - id: first\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n", "second,first"},
	} {
		cfg, err := parseMonitorConfig([]byte(tc.yaml + testExecutorRoleYAML))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if ids := classifierIDs(cfg.Roles["planner"].Classifiers); strings.Join(ids, ",") != tc.want {
			t.Errorf("classifier order = %v, want %s", ids, tc.want)
		}
		for _, c := range cfg.Roles["planner"].Classifiers {
			var choices []string
			for _, ch := range c.Choices {
				choices = append(choices, ch.ID)
			}
			if c.ID == "a" && strings.Join(choices, ",") != "x,y" {
				t.Errorf("choice order lost: %v", choices)
			}
		}
	}
}

// definitionsConfig decodes schmux's classifier definitions without
// validating them, so a test can remove a role and validate the rest.
func definitionsConfig(t *testing.T) MonitorConfig {
	t.Helper()
	var cfg MonitorConfig
	if err := yaml.Unmarshal(monitorDefinitionsYAML, &cfg); err != nil {
		t.Fatalf("decode flight-monitor.yaml: %v", err)
	}
	return cfg
}

func TestMonitorDefinitionsDefinePlannerAndExecutorClassifiers(t *testing.T) {
	if _, err := parseMonitorConfig(monitorDefinitionsYAML); err != nil {
		t.Fatalf("flight-monitor.yaml is invalid: %v", err)
	}
	cfg := definitionsConfig(t)
	planner := cfg.Roles[string(RolePlanner)].Classifiers
	// The planner does not check executed work, so it has no work_check.
	if got, want := classifierIDs(planner), []string{"attention", "planning_artifacts"}; !slices.Equal(got, want) {
		t.Errorf("planner classifier IDs = %v, want %v", got, want)
	}
	executor := cfg.Roles[string(RoleExecutor)].Classifiers
	if got, want := classifierIDs(executor), []string{"attention", "execution_state"}; !slices.Equal(got, want) {
		t.Errorf("executor classifier IDs = %v, want %v", got, want)
	}
	for _, c := range executor {
		if c.ID != "execution_state" {
			continue
		}
		if len(c.NotifyOn) != 0 || c.MinConfidence != 0 {
			t.Errorf("%s notify_on=%v min_confidence=%v, want none and 0", c.ID, c.NotifyOn, c.MinConfidence)
		}
	}
}

// The reviewer and committer roles carry the classifiers the review and
// completion phases read. Every reviewer turn notifies anyway, so their
// notify_on lists stay empty.
func TestMonitorDefinitionsDefineReviewerAndCommitClassifiers(t *testing.T) {
	cfg := definitionsConfig(t)
	reviewer := cfg.Roles[string(RoleReviewer)].Classifiers
	if got, want := classifierIDs(reviewer), []string{"review_state", "tests", "test_rules_review", "changed_work", "artifacts_named"}; !slices.Equal(got, want) {
		t.Errorf("reviewer classifier IDs = %v, want %v", got, want)
	}
	wantChoices := map[string][]string{
		"review_state":      {"finished", "fixing", "blocked"},
		"tests":             {"run_passed", "run_failed", "not_run"},
		"test_rules_review": {"compliant", "violations", "not_run"},
		"changed_work":      {"changed", "unchanged"},
		"artifacts_named":   {"named", "incomplete"},
	}
	for _, c := range reviewer {
		var choices []string
		for _, ch := range c.Choices {
			choices = append(choices, ch.ID)
		}
		if want := wantChoices[c.ID]; !slices.Equal(choices, want) {
			t.Errorf("reviewer %s choices = %v, want %v", c.ID, choices, want)
		}
		if len(c.NotifyOn) != 0 {
			t.Errorf("reviewer %s notify_on = %v, want empty", c.ID, c.NotifyOn)
		}
	}
	commit := cfg.Roles[string(RoleCommit)].Classifiers
	if got, want := classifierIDs(commit), []string{"attention", "completion_state"}; !slices.Equal(got, want) {
		t.Errorf("commit classifier IDs = %v, want %v", got, want)
	}
	for _, c := range commit {
		var choices []string
		for _, ch := range c.Choices {
			choices = append(choices, ch.ID)
		}
		switch c.ID {
		case "attention":
			if want := []string{"needs_attention", "routine"}; !slices.Equal(choices, want) {
				t.Errorf("commit attention choices = %v, want %v", choices, want)
			}
		case "completion_state":
			if want := []string{"committed", "gate_failed", "working", "blocked"}; !slices.Equal(choices, want) {
				t.Errorf("commit completion_state choices = %v, want %v", choices, want)
			}
			if len(c.NotifyOn) != 0 {
				t.Errorf("commit %s notify_on = %v, want empty", c.ID, c.NotifyOn)
			}
		}
	}
}

func TestValidateMonitorConfigRequiresPlannerAndExecutor(t *testing.T) {
	for _, missing := range []string{"planner", "executor", "reviewer", "commit"} {
		cfg := definitionsConfig(t)
		delete(cfg.Roles, missing)
		err := validateMonitorConfig(cfg)
		if err == nil || err.Error() != fmt.Sprintf("flight-monitor: role %q is required", missing) {
			t.Errorf("missing %s: err = %v", missing, err)
		}
	}
}

// TestMonitorRouting is the spec's routing table: any notify_on match, or any
// below-threshold confidence, must wake the pilot.
func TestMonitorRouting(t *testing.T) {
	rules := []MonitorClassifier{
		{ID: "attention", NotifyOn: []string{"needs_attention"}, MinConfidence: .7},
		{ID: "planning_artifacts", NotifyOn: []string{"spec", "plan", "both"}, MinConfidence: .7},
	}
	for _, tc := range []struct {
		name, attention, artifact string
		confidence                float64
		want                      bool
	}{
		{"routine", "routine", "neither", .9, false},
		{"question", "needs_attention", "neither", .9, true},
		{"artifact", "routine", "plan", .9, true},
		{"both", "needs_attention", "both", .9, true},
		{"uncertain", "routine", "neither", .4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := tc.confidence
			got := shouldNotify(rules, []MonitorAnswer{
				{ID: "attention", Choice: tc.attention, Confidence: &conf},
				{ID: "planning_artifacts", Choice: tc.artifact, Confidence: &conf},
			})
			if got != tc.want {
				t.Fatalf("notify=%v, want %v; case=%+v", got, tc.want, tc)
			}
		})
	}
}

func TestClassifyResponse_SendsOneRequestWithAuth(t *testing.T) {
	var captured struct {
		path    string
		method  string
		auth    string
		body    map[string]any
		bodyRaw []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.method = r.Method
		captured.auth = r.Header.Get("Authorization")
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		captured.bodyRaw = buf
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"attention":{"choice":"needs_attention","confidence":0.9,"probabilities":{"needs_attention":0.9,"routine":0.1}},"planning_artifacts":{"choice":"plan","confidence":0.8,"probabilities":{"spec":0.1,"plan":0.8,"both":0.05,"neither":0.05}}}}`))
	}))
	defer srv.Close()

	cfg := MonitorConfig{
		Model: "jev-latest",
		Roles: map[string]MonitorRole{
			"planner": {Classifiers: []MonitorClassifier{
				{ID: "attention", Question: "q", Choices: []MonitorChoice{{ID: "needs_attention", Description: "needs"}, {ID: "routine", Description: "rout"}}, NotifyOn: []string{"needs_attention"}, MinConfidence: 0.7},
				{ID: "planning_artifacts", Question: "q2", Choices: []MonitorChoice{{ID: "spec", Description: "s"}, {ID: "plan", Description: "p"}, {ID: "both", Description: "b"}, {ID: "neither", Description: "n"}}, NotifyOn: []string{"plan", "both", "spec"}, MinConfidence: 0.7},
			}},
		},
	}
	client := srv.Client()
	client.Timeout = 5 * time.Second

	// Temporarily redirect the endpoint to the test server.
	orig := monitorEndpoint
	monitorEndpoint = srv.URL + "/v1/systemone"
	t.Cleanup(func() { monitorEndpoint = orig })

	got, err := classifyResponse(httpTestContext(), client, "key-1", cfg, RolePlanner, "The planner asked a question and finished the plan.")
	if err != nil {
		t.Fatalf("classifyResponse() error = %v", err)
	}
	if captured.path != "/v1/systemone" {
		t.Errorf("path = %q, want /v1/systemone", captured.path)
	}
	if captured.method != http.MethodPost || captured.auth != "Bearer key-1" {
		t.Errorf("method/auth = %s/%s, want POST/Bearer key-1", captured.method, captured.auth)
	}
	if err := json.Unmarshal(captured.bodyRaw, &captured.body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	questions, _ := captured.body["questions"].(map[string]any)
	if len(questions) != 2 {
		t.Fatalf("questions = %+v, want two keyed objects", captured.body["questions"])
	}
	first := questions["attention"].(map[string]any)
	second := questions["planning_artifacts"].(map[string]any)
	if first["type"] != "choice" || second["instructions"] != "q2" {
		t.Fatalf("questions = %+v", questions)
	}
	criteria, ok := first["criteria"].(map[string]any)
	if !ok || criteria["needs_attention"] != "needs" || criteria["routine"] != "rout" {
		t.Fatalf("criteria = %+v", first["criteria"])
	}
	raw := string(captured.bodyRaw)
	if strings.Index(raw, `"needs_attention":"needs"`) > strings.Index(raw, `"routine":"rout"`) {
		t.Fatalf("choice order changed: %s", raw)
	}

	if captured.body["state"] != "The planner asked a question and finished the plan." {
		t.Errorf("state = %v", captured.body["state"])
	}
	if len(got.Answers) != 2 || got.Answers[0].ID != "attention" || got.Answers[1].ID != "planning_artifacts" {
		t.Errorf("answer order = %+v", got.Answers)
	}
	if !shouldNotify(cfg.Roles["planner"].Classifiers, got.Answers) {
		t.Error("routing should have notified on the returned answers")
	}
}

func TestClassifyResponse_FailureModes(t *testing.T) {
	cfg := MonitorConfig{
		Model: "m",
		Roles: map[string]MonitorRole{
			"planner": {Classifiers: []MonitorClassifier{
				{ID: "a", Question: "q", Choices: []MonitorChoice{{ID: "x", Description: "x"}, {ID: "y", Description: "y"}}, NotifyOn: []string{"x"}, MinConfidence: 0.7},
			}},
		},
	}

	t.Run("missing key", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		if _, err := classifyResponse(httpTestContext(), srv.Client(), "", cfg, RolePlanner, "x"); err == nil {
			t.Fatal("missing key should error")
		}
	})

	t.Run("executor role reaches the server", func(t *testing.T) {
		var requests int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"choice":"x","confidence":0.9}}}`))
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		execCfg := MonitorConfig{Model: "m", Roles: map[string]MonitorRole{"executor": cfg.Roles["planner"]}}
		got, err := classifyResponse(httpTestContext(), srv.Client(), "k", execCfg, RoleExecutor, "x")
		if err != nil || requests != 1 || len(got.Answers) != 1 {
			t.Fatalf("answers = %+v, requests = %d, err = %v", got.Answers, requests, err)
		}
	})

	t.Run("pilot role is unsupported", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Error("an unsupported role must not reach the server")
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		pilotCfg := MonitorConfig{Model: "m", Roles: map[string]MonitorRole{"pilot": cfg.Roles["planner"]}}
		_, err := classifyResponse(httpTestContext(), srv.Client(), "k", pilotCfg, RolePilot, "x")
		if err == nil || !strings.Contains(err.Error(), "not yet supported") {
			t.Fatalf("err = %v, want unsupported role", err)
		}
	})

	t.Run("api failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream down"))
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		_, err := classifyResponse(httpTestContext(), srv.Client(), "k", cfg, RolePlanner, "x")
		if err == nil || !strings.Contains(err.Error(), "http 500") {
			t.Fatalf("err = %v, want http 500", err)
		}
	})

	t.Run("missing answer id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"choice":"x","confidence":0.9}}}`))
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		_, err := classifyResponse(httpTestContext(), srv.Client(), "k", twoClassifierConfig(), RolePlanner, "x")
		if err == nil || !strings.Contains(err.Error(), "missing answer") {
			t.Fatalf("err = %v, want missing answer", err)
		}
	})

	t.Run("missing confidence", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"choice":"x"}}}`))
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		_, err := classifyResponse(httpTestContext(), srv.Client(), "k", cfg, RolePlanner, "x")
		if err == nil || !strings.Contains(err.Error(), "missing confidence") {
			t.Fatalf("err = %v, want missing confidence", err)
		}
	})

	t.Run("unknown choice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"choice":"z","confidence":0.9}}}`))
		}))
		defer srv.Close()
		orig := monitorEndpoint
		monitorEndpoint = srv.URL
		t.Cleanup(func() { monitorEndpoint = orig })
		_, err := classifyResponse(httpTestContext(), srv.Client(), "k", cfg, RolePlanner, "x")
		if err == nil || !strings.Contains(err.Error(), "unknown choice") {
			t.Fatalf("err = %v, want unknown choice", err)
		}
	})
}

func twoClassifierConfig() MonitorConfig {
	return MonitorConfig{
		Model: "m",
		Roles: map[string]MonitorRole{
			"planner": {Classifiers: []MonitorClassifier{
				{ID: "a", Question: "q", Choices: []MonitorChoice{{ID: "x", Description: "x"}, {ID: "y", Description: "y"}}, NotifyOn: []string{"x"}, MinConfidence: 0.7},
				{ID: "b", Question: "q", Choices: []MonitorChoice{{ID: "x", Description: "x"}, {ID: "y", Description: "y"}}, NotifyOn: []string{"x"}, MinConfidence: 0.7},
			}},
		},
	}
}

func httpTestContext() context.Context { return context.Background() }

func classifierIDs(defs []MonitorClassifier) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.ID)
	}
	return out
}
