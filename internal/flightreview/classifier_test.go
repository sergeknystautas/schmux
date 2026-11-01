package flightreview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

const testKey = "test-secret-key"

// jevServer answers every request with the given choice and counts requests.
func jevServer(t *testing.T, choice string, confidence float64, requests *atomic.Int32, bodies *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if bodies != nil {
			buf := new(strings.Builder)
			_, _ = copyBody(buf, r)
			*bodies = append(*bodies, buf.String()+"|auth="+r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "jev-test",
			"answers": map[string]any{
				chatTaxonomyQuestionID: map[string]any{"choice": choice, "confidence": confidence},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func copyBody(dst *strings.Builder, r *http.Request) (int64, error) {
	buf := make([]byte, 4096)
	var total int64
	for {
		n, err := r.Body.Read(buf)
		dst.Write(buf[:n])
		total += int64(n)
		if err != nil {
			return total, nil
		}
	}
}

func saveKey(t *testing.T) {
	t.Helper()
	if err := config.SaveTypesafeAPIKey(testKey); err != nil {
		t.Fatal(err)
	}
}

// writeConversationAt writes a session log with explicit timestamps.
func writeConversationAt(t *testing.T, workspaceID, sessionID string, records ...chat.Record) {
	t.Helper()
	log, err := chat.OpenLog(chat.ConversationPath(schmuxdir.ChatSessionDir(workspaceID, sessionID)))
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range records {
		if err := log.Append(rec); err != nil {
			t.Fatal(err)
		}
	}
}

// classifiableFlight archives a started flight whose planner transcript holds
// one launch prompt and one operator message, and returns a service wired to
// the given classifier.
func classifiableFlight(t *testing.T, classifier ChatClassifier) *Service {
	t.Helper()
	isolate(t)
	row := startedRow("fl-1", flights.StateExecuting)
	assign(&row, flights.RolePlanner, "s-plan")
	row.Sessions[0].UpdatedAt = at(2)
	harness := chat.Record{Ts: at(5).Format(time.RFC3339Nano), Type: chat.RecordHarness,
		Line: json.RawMessage(`{"type":"assistant","message":{"content":[{"type":"text","text":"I will use sqlite."}]}}`)}
	writeConversationAt(t, "ws-fl-1", "s-plan",
		userMsg("launch", "plan it", at(2).Add(-time.Second)),
		harness,
		userMsg("chat-1", "No, use postgres instead.", at(6)),
	)
	svc := NewService(Options{Now: func() time.Time { return at(200) }, Version: "test", Classifier: classifier})
	if err := svc.Capture(context.Background(), row, false); err != nil {
		t.Fatal(err)
	}
	return svc
}

func readAttempts(t *testing.T) ([]ClassificationAttempt, []flighthistory.MalformedLine) {
	t.Helper()
	attempts, malformed, err := readClassifications("fl-1")
	if err != nil {
		t.Fatal(err)
	}
	return attempts, malformed
}

func TestJevClassifierMapsAuditableChoiceLabel(t *testing.T) {
	isolate(t)
	saveKey(t)
	var requests atomic.Int32
	var bodies []string
	server := jevServer(t, "routing_correction", 0.8, &requests, &bodies)

	label, confidence, model, err := NewJevClassifier(server.Client(), server.URL).Classify(context.Background(), "wrong recipient")

	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if label != LabelRouting || confidence != 0.8 || model != "jev-test" {
		t.Errorf("label=%s confidence=%v model=%s", label, confidence, model)
	}
	if requests.Load() != 1 || !strings.Contains(bodies[0], "auth=Bearer "+testKey) || !strings.Contains(bodies[0], "wrong recipient") {
		t.Errorf("request = %v", bodies)
	}
	for _, l := range allLabels {
		if !strings.Contains(bodies[0], string(l)) {
			t.Errorf("request does not offer the %q label", l)
		}
	}

	t.Run("an unknown choice is an error, not a label", func(t *testing.T) {
		var n atomic.Int32
		bogus := jevServer(t, "made_up", 0.9, &n, nil)
		if _, _, _, err := NewJevClassifier(bogus.Client(), bogus.URL).Classify(context.Background(), "x"); err == nil {
			t.Error("an unknown Jev choice was accepted")
		}
	})
	t.Run("a failed request does not echo the endpoint or key", func(t *testing.T) {
		var n atomic.Int32
		gone := jevServer(t, "approval", 0.9, &n, nil)
		url, client := gone.URL, gone.Client()
		gone.Close()
		_, _, _, err := NewJevClassifier(client, url).Classify(context.Background(), "x")
		if err == nil {
			t.Fatal("a closed server produced a label")
		}
		if strings.Contains(err.Error(), url) || strings.Contains(err.Error(), testKey) {
			t.Errorf("error leaks the endpoint or key: %v", err)
		}
	})
	t.Run("a missing key fails without a request", func(t *testing.T) {
		if err := config.SaveTypesafeAPIKey(""); err != nil {
			t.Fatal(err)
		}
		var n atomic.Int32
		quiet := jevServer(t, "approval", 0.9, &n, nil)
		if _, _, _, err := NewJevClassifier(quiet.Client(), quiet.URL).Classify(context.Background(), "x"); err == nil || n.Load() != 0 {
			t.Errorf("err = %v, requests = %d", err, n.Load())
		}
	})
}

func TestClassifyInterventionsSkipsExistingAttempt(t *testing.T) {
	var requests atomic.Int32
	server := jevServer(t, "workflow_correction", 0.9, &requests, nil)
	svc := classifiableFlight(t, NewJevClassifier(server.Client(), server.URL))
	saveKey(t)

	for i := 0; i < 2; i++ {
		if err := svc.ClassifyInterventions(context.Background(), "fl-1", false); err != nil {
			t.Fatalf("ClassifyInterventions #%d: %v", i+1, err)
		}
	}

	if requests.Load() != 1 {
		t.Errorf("Jev requests = %d, want 1 (the second run reuses the stored attempt)", requests.Load())
	}
	attempts, malformed := readAttempts(t)
	if len(attempts) != 1 || len(malformed) != 0 {
		t.Fatalf("attempts = %+v malformed = %v", attempts, malformed)
	}
	a := attempts[0]
	if a.Choice != LabelWorkflow || a.Confidence == nil || *a.Confidence != 0.9 || a.Model != "jev-test" || a.Error != "" {
		t.Errorf("attempt = %+v", a)
	}
	if a.ChatRecordID != "chat-1" || a.InterventionEventID != "iv-chat-s-plan-3" || len(a.DefinitionSHA256) != 64 || a.ID == "" || !a.CreatedAt.Equal(at(200)) {
		t.Errorf("attempt identity = %+v", a)
	}
	if !strings.Contains(a.Excerpt, "No, use postgres instead.") || !strings.Contains(a.Excerpt, "I will use sqlite.") {
		t.Errorf("excerpt = %q, want the operator text with its preceding context", a.Excerpt)
	}
	path := filepath.Join(archiveDir(t, "fl-1"), "classifications.jsonl")
	if mode := modeOf(t, path); mode != 0o600 {
		t.Errorf("classifications.jsonl mode = %v, want 0600", mode)
	}
	raw, _ := os.ReadFile(path)
	for _, secret := range []string{testKey, "Authorization", "Bearer", server.URL} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("classifications.jsonl leaks %q", secret)
		}
	}

	_, summary := readSummaryFile(t, "fl-1")
	if summary.Counts.ByLabel[string(LabelWorkflow)] != 1 || summary.Counts.Unlabeled != 0 {
		t.Errorf("summary labels = %v unlabeled = %d, want the stored label counted", summary.Counts.ByLabel, summary.Counts.Unlabeled)
	}
}

func TestClassifyInterventionsForceAppendsNewAttempt(t *testing.T) {
	var requests atomic.Int32
	server := jevServer(t, "product_guidance", 0.7, &requests, nil)
	svc := classifiableFlight(t, NewJevClassifier(server.Client(), server.URL))
	saveKey(t)
	if err := svc.ClassifyInterventions(context.Background(), "fl-1", false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(archiveDir(t, "fl-1"), "classifications.jsonl")
	before, _ := os.ReadFile(path)

	if err := svc.ClassifyInterventions(context.Background(), "fl-1", true); err != nil {
		t.Fatal(err)
	}

	if requests.Load() != 2 {
		t.Errorf("Jev requests = %d, want 2", requests.Load())
	}
	attempts, _ := readAttempts(t)
	if len(attempts) != 2 || attempts[0].ID == attempts[1].ID {
		t.Fatalf("attempts = %+v, want two distinct", attempts)
	}
	after, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Error("force rewrote the first attempt instead of appending")
	}
}

func TestClassifyInterventionsRecordsErrorWithoutKey(t *testing.T) {
	var requests atomic.Int32
	server := jevServer(t, "approval", 0.9, &requests, nil)
	svc := classifiableFlight(t, NewJevClassifier(server.Client(), server.URL))
	// No key is saved.

	if err := svc.ClassifyInterventions(context.Background(), "fl-1", false); err != nil {
		t.Fatalf("a missing key must not fail report generation: %v", err)
	}

	if requests.Load() != 0 {
		t.Errorf("Jev requests = %d, want 0 without a key", requests.Load())
	}
	attempts, _ := readAttempts(t)
	if len(attempts) != 1 || attempts[0].Error == "" || attempts[0].Choice != "" || attempts[0].Confidence != nil {
		t.Fatalf("attempts = %+v, want one visible failed attempt", attempts)
	}
	_, summary := readSummaryFile(t, "fl-1")
	if summary.Counts.Unlabeled != 1 || len(summary.Counts.ByLabel) != 0 {
		t.Errorf("unlabeled = %d labels = %v, want the failed message left unlabeled", summary.Counts.Unlabeled, summary.Counts.ByLabel)
	}
}

func TestClassifyInterventionsKeepsMalformedClassificationLines(t *testing.T) {
	var requests atomic.Int32
	server := jevServer(t, "approval", 0.9, &requests, nil)
	svc := classifiableFlight(t, NewJevClassifier(server.Client(), server.URL))
	saveKey(t)
	path := filepath.Join(archiveDir(t, "fl-1"), "classifications.jsonl")
	if err := os.WriteFile(path, []byte("garbage line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := svc.ClassifyInterventions(context.Background(), "fl-1", false); err != nil {
		t.Fatal(err)
	}

	attempts, malformed := readAttempts(t)
	if len(attempts) != 1 || len(malformed) != 1 || malformed[0].Line != 1 {
		t.Errorf("attempts = %d malformed = %+v", len(attempts), malformed)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), "garbage line\n") {
		t.Errorf("malformed line was not preserved: %q", raw)
	}
}

func TestClassificationExcerptTruncatesOnRuneBoundary(t *testing.T) {
	euro := strings.Repeat("€", 1500)    // 4500 bytes of three-byte runes
	accented := strings.Repeat("é", 400) // 800 bytes of two-byte runes

	got := buildExcerpt(accented, euro)

	if !utf8.ValidString(got) {
		t.Fatal("excerpt is not valid UTF-8")
	}
	const contextHeader, operatorHeader = "Previous message:\n", "\n\nOperator message:\n"
	operatorAt := strings.Index(got, operatorHeader)
	contextPart := got[len(contextHeader):operatorAt]
	operator := got[operatorAt+len(operatorHeader):]
	// 2000 bytes holds 666 three-byte runes (1998 bytes); 500 holds 250 two-byte runes.
	if len(operator) != 1998 || len(contextPart) != 500 {
		t.Errorf("operator = %d bytes, context = %d bytes; want 1998 and 500", len(operator), len(contextPart))
	}
	if short := buildExcerpt("", "ok"); !strings.Contains(short, "ok") || strings.Contains(short, "Previous message:") {
		t.Errorf("excerpt without context = %q", short)
	}
}

func TestDeriveSummaryCountsLatestSuccessfulLabelAndLowConfidence(t *testing.T) {
	f := executingFlight()
	conf := func(v float64) *float64 { return &v }
	ev := Evidence{Flight: f, Transcripts: []Transcript{transcript("planner", "s-plan",
		userMsg("launch", "go", at(2).Add(-time.Second)),
		userMsg("chat-a", "one", at(10)),
		userMsg("chat-b", "two", at(11)),
		userMsg("chat-c", "three", at(12)),
	)}, Classifications: []ClassificationAttempt{
		{ID: "c1", InterventionEventID: "iv-chat-s-plan-2", Choice: LabelStatus, Confidence: conf(0.9), CreatedAt: at(20)},
		{ID: "c2", InterventionEventID: "iv-chat-s-plan-2", Choice: LabelRouting, Confidence: conf(0.9), CreatedAt: at(30)},
		{ID: "c3", InterventionEventID: "iv-chat-s-plan-3", Choice: LabelUnclear, Confidence: conf(0.4), CreatedAt: at(20)},
		{ID: "c4", InterventionEventID: "iv-chat-s-plan-3", Error: "jev down", CreatedAt: at(40)},
		{ID: "c5", InterventionEventID: "iv-chat-s-plan-4", Error: "jev down", CreatedAt: at(20)},
	}}

	got := DeriveSummary(ev)

	if got.Counts.ByLabel[string(LabelRouting)] != 1 || got.Counts.ByLabel[string(LabelStatus)] != 0 {
		t.Errorf("labels = %v, want the latest successful attempt per message", got.Counts.ByLabel)
	}
	if got.Counts.ByLabel[string(LabelUnclear)] != 1 {
		t.Errorf("unclear was collapsed into another bucket: %v", got.Counts.ByLabel)
	}
	if got.Counts.Unlabeled != 1 || got.Counts.LowConfidence != 1 {
		t.Errorf("unlabeled = %d low confidence = %d, want 1 and 1", got.Counts.Unlabeled, got.Counts.LowConfidence)
	}
}
