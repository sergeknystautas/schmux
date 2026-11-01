package flightreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
)

// ChatLabel is the primary purpose of an operator message.
type ChatLabel string

const (
	LabelProductGuidance ChatLabel = "product_guidance"
	LabelRouting         ChatLabel = "routing_correction"
	LabelWorkflow        ChatLabel = "workflow_correction"
	LabelEnvironment     ChatLabel = "environment_unblock"
	LabelApproval        ChatLabel = "approval"
	LabelStatus          ChatLabel = "status_query"
	LabelManualTakeover  ChatLabel = "manual_takeover"
	LabelMixed           ChatLabel = "mixed"
	LabelUnclear         ChatLabel = "unclear"
)

// LowConfidenceThreshold is the confidence below which a stored label is
// counted separately as low confidence.
const LowConfidenceThreshold = 0.6

// chatTaxonomyQuestionID names the one Choice question asked of Jev.
const chatTaxonomyQuestionID = "operator_chat_purpose"

// Excerpt limits: the operator's text and the context before it, in bytes.
const (
	excerptOperatorBytes = 2000
	excerptContextBytes  = 500
)

var chatLabelDescriptions = []struct {
	Label       ChatLabel
	Description string
}{
	{LabelProductGuidance, "Requirements, priorities, scope, or a quality judgement about what is being built."},
	{LabelRouting, "The wrong recipient, a stale session, a phase mismatch, or a message that belonged to another role."},
	{LabelWorkflow, "Undoing work, overriding the phase, choosing the next reviewer, or manually sequencing the workflow."},
	{LabelEnvironment, "A permission, missing tool, credential, daemon, or filesystem problem blocking the agent."},
	{LabelApproval, "An explicit permission or approval of a destructive or gated action."},
	{LabelStatus, "Asking what happened or what the agent is doing."},
	{LabelManualTakeover, "Saying the user will do the work directly instead of the agent."},
	{LabelMixed, "More than one of these purposes is equally central."},
	{LabelUnclear, "Low confidence, or not enough context to tell."},
}

// allLabels lists every label the taxonomy offers.
var allLabels = func() []ChatLabel {
	labels := make([]ChatLabel, len(chatLabelDescriptions))
	for i, d := range chatLabelDescriptions {
		labels[i] = d.Label
	}
	return labels
}()

// taxonomyConfig is the one-question Choice definition sent to Jev. It reuses
// the monitor's request format under the reviewer role only because the config
// is passed explicitly; it is never installed into flight-monitor.yaml.
func taxonomyConfig() flights.MonitorConfig {
	choices := make([]flights.MonitorChoice, len(chatLabelDescriptions))
	for i, d := range chatLabelDescriptions {
		choices[i] = flights.MonitorChoice{ID: string(d.Label), Description: d.Description}
	}
	return flights.MonitorConfig{
		Model: "jev-latest",
		Roles: map[string]flights.MonitorRole{string(flights.RoleReviewer): {Classifiers: []flights.MonitorClassifier{{
			ID:       chatTaxonomyQuestionID,
			Question: "What was the operator's primary purpose in writing the operator message to a coding agent? Choose the single purpose that is most central.",
			Choices:  choices,
		}}}},
	}
}

// taxonomyDigest identifies the exact question and choices a label answered.
func taxonomyDigest() string {
	data, _ := json.Marshal(taxonomyConfig())
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ClassificationAttempt is one immutable Jev call about one operator message.
// It never holds credentials or the endpoint.
type ClassificationAttempt struct {
	ID                  string    `json:"id"`
	InterventionEventID string    `json:"intervention_event_id"`
	ChatRecordID        string    `json:"chat_record_id"`
	Excerpt             string    `json:"excerpt"`
	Model               string    `json:"model"`
	DefinitionSHA256    string    `json:"definition_sha256"`
	Choice              ChatLabel `json:"choice"`
	Confidence          *float64  `json:"confidence,omitempty"`
	Error               string    `json:"error,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// ChatClassifier labels one operator message excerpt.
type ChatClassifier interface {
	Classify(ctx context.Context, excerpt string) (label ChatLabel, confidence float64, model string, err error)
}

type jevClassifier struct {
	client   *http.Client
	endpoint string
}

// NewJevClassifier asks Jev's Choice endpoint. The API key is read at call
// time and never retained.
func NewJevClassifier(client *http.Client, endpoint string) ChatClassifier {
	return &jevClassifier{client: client, endpoint: endpoint}
}

func (c *jevClassifier) Classify(ctx context.Context, excerpt string) (ChatLabel, float64, string, error) {
	key, err := config.GetTypesafeAPIKey()
	if err != nil {
		return "", 0, "", fmt.Errorf("read TypeSafe API key: %w", err)
	}
	if key == "" {
		return "", 0, "", errors.New("flight-review: typesafe API key is not configured; set it in Settings > Flights > Pilot Supervision")
	}
	cfg := taxonomyConfig()
	result, err := flights.ClassifyChoice(ctx, c.client, c.endpoint, key, cfg, flights.RoleReviewer, excerpt)
	if err != nil {
		return "", 0, cfg.Model, c.scrub(err, key)
	}
	if len(result.Answers) != 1 || result.Answers[0].Confidence == nil {
		return "", 0, result.Model, errors.New("flight-review: jev returned no usable answer")
	}
	return ChatLabel(result.Answers[0].Choice), *result.Answers[0].Confidence, result.Model, nil
}

// scrub removes the endpoint and key from an error before it is stored.
func (c *jevClassifier) scrub(err error, key string) error {
	msg := err.Error()
	if c.endpoint != "" {
		msg = strings.ReplaceAll(msg, c.endpoint, "[endpoint]")
	}
	return errors.New(strings.ReplaceAll(msg, key, "[redacted]"))
}

// cutTail returns the last max bytes of s, starting on a rune boundary.
func cutTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	start := len(s) - max
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// cutHead returns the first max bytes of s, ending on a rune boundary.
func cutHead(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// buildExcerpt is what Jev sees: the operator's text, capped, preceded by the
// tail of the message before it.
func buildExcerpt(previous, operator string) string {
	operator = cutHead(strings.TrimSpace(operator), excerptOperatorBytes)
	previous = cutTail(strings.TrimSpace(previous), excerptContextBytes)
	if previous == "" {
		return "Operator message:\n" + operator
	}
	return "Previous message:\n" + previous + "\n\nOperator message:\n" + operator
}

// recordText extracts readable text from a conversation record for context:
// a user message's text, or an assistant harness line's text blocks.
func recordText(rec chat.Record) string {
	switch rec.Type {
	case chat.RecordUserMessage:
		return rec.Text
	case chat.RecordHarness:
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(rec.Line, &line) != nil || line.Type != "assistant" {
			return ""
		}
		var parts []string
		for _, block := range line.Message.Content {
			if block.Type == "text" && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// excerptFor finds the operator message behind an intervention and builds the
// text sent to Jev.
func excerptFor(transcripts []Transcript, iv Intervention) (string, bool) {
	for _, tr := range transcripts {
		if tr.SessionID != iv.SessionID {
			continue
		}
		for i, rec := range tr.Records {
			if rec.Record.ID != iv.ChatRecordID || rec.Record.Type != chat.RecordUserMessage {
				continue
			}
			previous := ""
			for j := i - 1; j >= 0 && previous == ""; j-- {
				previous = recordText(tr.Records[j].Record)
			}
			return buildExcerpt(previous, rec.Record.Text), true
		}
	}
	return "", false
}

func classificationsPath(flightID string) (string, error) {
	dir, err := flighthistory.FlightDir(flightID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "classifications.jsonl"), nil
}

// readClassifications reads a flight's stored attempts, keeping malformed
// lines visible.
func readClassifications(flightID string) ([]ClassificationAttempt, []flighthistory.MalformedLine, error) {
	path, err := classificationsPath(flightID)
	if err != nil {
		return nil, nil, err
	}
	var attempts []ClassificationAttempt
	malformed, err := flighthistory.ReadJSONLines(path, func(line []byte) error {
		var a ClassificationAttempt
		if err := json.Unmarshal(line, &a); err != nil {
			return err
		}
		attempts = append(attempts, a)
		return nil
	})
	return attempts, malformed, err
}

// ClassifyInterventions labels the operator messages a flight's user wrote
// after takeoff. Each call to Jev is stored as an immutable attempt; a message
// that already has an attempt is not asked again unless force is set, and a
// forced run appends rather than replaces. A missing key or a Jev error is
// stored as a failed attempt and does not fail the call. The flight's summary
// is rebuilt afterwards so its label counts include the new attempts.
func (s *Service) ClassifyInterventions(ctx context.Context, flightID string, force bool) error {
	l, err := readLedgers()
	if err != nil {
		return err
	}
	ev, err := loadEvidence(flightID, l, nil)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, a := range ev.Classifications {
		have[a.InterventionEventID] = true
	}

	var fresh []any
	for _, iv := range DeriveSummary(ev).Interventions {
		if iv.Kind != KindOperatorMessage || iv.ChatRecordID == "" || (have[iv.ID] && !force) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		excerpt, ok := excerptFor(ev.Transcripts, iv)
		if !ok {
			continue
		}
		fresh = append(fresh, s.classifyOne(ctx, iv, excerpt))
	}
	if len(fresh) > 0 {
		path, err := classificationsPath(flightID)
		if err != nil {
			return err
		}
		if err := flighthistory.AppendJSONLines(path, fresh); err != nil {
			return fmt.Errorf("store classifications for flight %s: %w", flightID, err)
		}
	}
	return s.rebuildOne(flightID, l, nil)
}

func (s *Service) classifyOne(ctx context.Context, iv Intervention, excerpt string) ClassificationAttempt {
	attempt := ClassificationAttempt{
		ID: randomID("ca-"), InterventionEventID: iv.ID, ChatRecordID: iv.ChatRecordID, Excerpt: excerpt,
		DefinitionSHA256: taxonomyDigest(), CreatedAt: s.opts.Now().UTC(),
	}
	label, confidence, model, err := s.opts.Classifier.Classify(ctx, excerpt)
	attempt.Model = model
	if err != nil {
		attempt.Error = err.Error()
		return attempt
	}
	attempt.Choice, attempt.Confidence = label, &confidence
	return attempt
}

// latestLabels maps each intervention to its newest successful attempt.
func latestLabels(attempts []ClassificationAttempt) map[string]ClassificationAttempt {
	sorted := append([]ClassificationAttempt(nil), attempts...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) })
	latest := map[string]ClassificationAttempt{}
	for _, a := range sorted {
		if a.Error == "" && a.Choice != "" {
			latest[a.InterventionEventID] = a
		}
	}
	return latest
}
