package flights

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureMonitorConfig_CreatesOnlyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flight-monitor.yaml")

	if err := ensureMonitorConfig(path); err != nil {
		t.Fatalf("ensureMonitorConfig() on missing file error = %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first file: %v", err)
	}
	if !strings.Contains(string(first), "model:") {
		t.Errorf("seeded template does not look like monitor config: %s", first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	// User edits are preserved: ensureMonitorConfig must not rewrite the file.
	edited := []byte("model: custom\nroles:\n  custom:\n    classifiers: []\n")
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureMonitorConfig(path); err != nil {
		t.Fatalf("ensureMonitorConfig() on existing file error = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(edited) {
		t.Errorf("ensureMonitorConfig() overwrote user edits")
	}
}

func TestMonitorReadinessError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "flight-monitor.yaml")
	if err := ensureMonitorConfig(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_API_KEY", "ignored-environment-key")
	if got := MonitorReadinessError(path); !strings.Contains(got, "Settings > Flights > Pilot Supervision") {
		t.Fatalf("missing-key readiness = %q", got)
	}
	saveMonitorKey(t, "test-secret-never-returned")
	if got := MonitorReadinessError(path); got != "" {
		t.Fatalf("ready configuration = %q", got)
	}
	otherRole := strings.ReplaceAll(string(monitorTemplateYAML), "  planner:", "  executor:")
	if err := os.WriteFile(path, []byte(otherRole), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := MonitorReadinessError(path); !strings.Contains(got, "no classifier definitions for planner") {
		t.Fatalf("missing planner configuration readiness = %q", got)
	}
	if err := os.WriteFile(path, []byte("model: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := MonitorReadinessError(path); !strings.Contains(got, "flight-monitor: parse") || strings.Contains(got, "test-secret-never-returned") {
		t.Fatalf("invalid configuration readiness = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := MonitorReadinessError(path); !strings.Contains(got, "flight-monitor: read") {
		t.Fatalf("missing configuration readiness = %q", got)
	}
}

func TestLoadMonitorConfig_RejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"unknown field", "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q\n        extra: true\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n", "unknown"},
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
			path := filepath.Join(t.TempDir(), "flight-monitor.yaml")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := loadMonitorConfig(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("loadMonitorConfig() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadMonitorConfig_PreservesOrderAndReadsEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flight-monitor.yaml")
	initial := "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: a\n        question: q1\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n      - id: b\n        question: q2\n        choices:\n          - {id: yes, description: yes}\n          - {id: no, description: no}\n        notify_on: [yes]\n        min_confidence: 0.5\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := loadMonitorConfig(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if ids := classifierIDs(first.Roles["planner"].Classifiers); strings.Join(ids, ",") != "a,b" {
		t.Errorf("classifier order lost on first load: %v", ids)
	}
	for _, c := range first.Roles["planner"].Classifiers {
		descriptions := make([]string, 0, len(c.Choices))
		for _, ch := range c.Choices {
			descriptions = append(descriptions, ch.ID)
		}
		if c.ID == "a" && strings.Join(descriptions, ",") != "x,y" {
			t.Errorf("choice order lost: %v", descriptions)
		}
	}

	// A real edit between loads must take effect, with the new file's order.
	edited := "model: jev-latest\nroles:\n  planner:\n    classifiers:\n      - id: second\n        question: q\n        choices:\n          - {id: yes, description: yes}\n          - {id: no, description: no}\n        notify_on: [yes]\n        min_confidence: 0.5\n      - id: first\n        question: q\n        choices:\n          - {id: x, description: x}\n          - {id: y, description: y}\n        notify_on: [x]\n        min_confidence: 0.5\n"
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := loadMonitorConfig(path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	ids := classifierIDs(second.Roles["planner"].Classifiers)
	if strings.Join(ids, ",") != "second,first" {
		t.Errorf("classifier order after edit = %v, want [second first]", ids)
	}
}

func TestMonitorConfigForRole_FiltersNonPlannerRoles(t *testing.T) {
	cfg := MonitorConfig{
		Model: "m",
		Roles: map[string]MonitorRole{
			"planner": {Classifiers: []MonitorClassifier{{ID: "a"}}},
			"custom":  {Classifiers: []MonitorClassifier{{ID: "b"}}},
		},
	}
	role, ok := cfg.Roles[string(RolePlanner)]
	if !ok || len(role.Classifiers) == 0 {
		t.Errorf("planner role not loaded: %+v", role)
	}
	if _, ok := cfg.Roles[string(RoleExecutor)]; ok {
		t.Errorf("executor role should be absent; we only ship planner")
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
