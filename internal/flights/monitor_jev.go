package flights

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// monitorEndpoint is the Jev HTTP endpoint schmux talks to. The path is the
// same one the spike used. Tests in this package override the value to point
// at an httptest.Server.
var monitorEndpoint = "https://api.typesafe.ai/v1/systemone"

// monitorHTTPTimeout bounds one classification request. A successful call
// returns in well under this; a slow key or Jev outage fails the request and
// surfaces the error to the user rather than blocking the monitor worker.
const monitorHTTPTimeout = 30 * time.Second

// classifyResponse sends one HTTP request to Jev for a single completed
// planner response, returning structured answers in the role's question order.
// A missing key, a non-2xx response, malformed JSON, an unknown question or
// choice, or a missing confidence all return errors: they are visible
// classification failures, not routine rulings.
func classifyResponse(ctx context.Context, client *http.Client, key string, config MonitorConfig, role SessionRole, text string) (Classification, error) {
	if client == nil {
		return Classification{}, errors.New("flight-monitor: http client is nil")
	}
	roleDef, ok := config.Roles[string(role)]
	if !ok {
		return Classification{}, fmt.Errorf("flight-monitor: no definitions for role %q", role)
	}
	if len(roleDef.Classifiers) == 0 {
		return Classification{}, fmt.Errorf("flight-monitor: role %q has no classifiers", role)
	}
	apiKey := strings.TrimSpace(key)
	if apiKey == "" {
		return Classification{}, errors.New("flight-monitor: TypeSafe API key is not configured; set it in Settings > Flights > Pilot Supervision")
	}

	body, err := buildJevRequest(config, role, roleDef, text)
	if err != nil {
		return Classification{}, err
	}

	httpCtx, cancel := context.WithTimeout(ctx, monitorHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost, monitorEndpoint, bytes.NewReader(body))
	if err != nil {
		return Classification{}, fmt.Errorf("flight-monitor: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Classification{}, fmt.Errorf("flight-monitor: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Classification{}, fmt.Errorf("flight-monitor: read response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return Classification{}, fmt.Errorf("flight-monitor: http %d: %s", resp.StatusCode, firstLine(string(respBody)))
	}

	classification, err := parseJevResponse(roleDef.Classifiers, respBody)
	if classification.Model == "" {
		classification.Model = config.Model
	}
	return classification, err
}

// buildJevRequest renders the body Jev expects. The criteria object is built
// in YAML order so the model's answer maps back to our definitions; Go map
// serialization would otherwise reorder them.
func buildJevRequest(config MonitorConfig, role SessionRole, roleDef MonitorRole, text string) ([]byte, error) {
	// Build keyed objects explicitly: encoding a map would reorder choices.
	var questions bytes.Buffer
	questions.WriteByte('{')
	for i, c := range roleDef.Classifiers {
		if i > 0 {
			questions.WriteByte(',')
		}
		id, _ := json.Marshal(c.ID)
		instructions, _ := json.Marshal(c.Question)
		questions.Write(id)
		questions.WriteString(`:{"type":"choice","instructions":`)
		questions.Write(instructions)
		questions.WriteString(`,"criteria":{`)
		for j, ch := range c.Choices {
			if j > 0 {
				questions.WriteByte(',')
			}
			key, _ := json.Marshal(ch.ID)
			description, _ := json.Marshal(ch.Description)
			questions.Write(key)
			questions.WriteByte(':')
			questions.Write(description)
		}
		questions.WriteString(`}}`)
	}
	questions.WriteByte('}')
	body := struct {
		Model     string          `json:"model"`
		State     string          `json:"state"`
		Questions json.RawMessage `json:"questions"`
	}{config.Model, text, questions.Bytes()}

	switch role {
	case RolePlanner, RoleExecutor, RoleReviewer, RoleCommit:
		// Every role the flight supervises asks Choice questions only.
	default:
		// Other roles would need their own Question type definitions.
		return nil, fmt.Errorf("flight-monitor: role %q is not yet supported", role)
	}
	return json.Marshal(body)
}

// jevWireResponse is the partial shape of the Jev Choice answer. The
// probabilities and confidence fields are pointers so the JSON parser can
// distinguish "field absent" (a missing confidence, which is a classification
// error) from a literal zero.
type jevWireResponse struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Choice        string             `json:"choice"`
		Confidence    *float64           `json:"confidence"`
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
}

// parseJevResponse maps the wire response to our definition order, rejecting
// missing or extra question IDs and answers whose choice is not one of the
// classifier's choices. Every answer must include a confidence value.
func parseJevResponse(defs []MonitorClassifier, raw []byte) (Classification, error) {
	var resp jevWireResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Classification{}, fmt.Errorf("flight-monitor: decode response: %w", err)
	}
	if resp.Model != "" {
		// Some endpoints echo the model. Trust it if present; an absent model
		// is fine when the API omits it.
		_ = resp.Model
	}
	if len(resp.Answers) != len(defs) {
		// Find the first definition whose answer is absent and surface that,
		// so the operator sees which classifier's answer went missing.
		for _, def := range defs {
			if _, ok := resp.Answers[def.ID]; !ok {
				return Classification{}, fmt.Errorf("flight-monitor: response missing answer for %q", def.ID)
			}
		}
		// The count diverges but every definition is present — shouldn't
		// happen with well-formed inputs, but the error must not be silent.
		return Classification{}, fmt.Errorf("flight-monitor: response has %d answers, want %d", len(resp.Answers), len(defs))
	}
	out := Classification{Model: resp.Model, Answers: make([]MonitorAnswer, 0, len(defs))}
	for _, def := range defs {
		ans, ok := resp.Answers[def.ID]
		if !ok {
			return Classification{}, fmt.Errorf("flight-monitor: response missing answer for %q", def.ID)
		}
		if ans.Choice == "" {
			return Classification{}, fmt.Errorf("flight-monitor: answer %q has empty choice", def.ID)
		}
		if !choiceExists(def, ans.Choice) {
			return Classification{}, fmt.Errorf("flight-monitor: answer %q returned unknown choice %q", def.ID, ans.Choice)
		}
		if ans.Confidence == nil {
			return Classification{}, fmt.Errorf("flight-monitor: answer %q missing confidence", def.ID)
		}
		if *ans.Confidence < 0 || *ans.Confidence > 1 {
			return Classification{}, fmt.Errorf("flight-monitor: answer %q has invalid confidence %v", def.ID, *ans.Confidence)
		}
		out.Answers = append(out.Answers, MonitorAnswer{
			ID:            def.ID,
			Choice:        ans.Choice,
			Confidence:    ans.Confidence,
			Probabilities: ans.Probabilities,
		})
	}
	return out, nil
}

func choiceExists(def MonitorClassifier, id string) bool {
	for _, ch := range def.Choices {
		if ch.ID == id {
			return true
		}
	}
	return false
}

// shouldNotify returns true if any answer's choice is in its classifier's
// notify_on list, or any answer's confidence is below its classifier's
// min_confidence threshold. Returning true means the pilot should be told.
func shouldNotify(defs []MonitorClassifier, answers []MonitorAnswer) bool {
	if len(defs) == 0 || len(answers) == 0 {
		return false
	}
	byID := make(map[string]MonitorClassifier, len(defs))
	for _, d := range defs {
		byID[d.ID] = d
	}
	for _, a := range answers {
		def, ok := byID[a.ID]
		if !ok {
			// An answer for an unknown classifier is always worth surfacing
			// so the operator notices the drift between the YAML and the
			// response.
			return true
		}
		if a.Confidence != nil && *a.Confidence < def.MinConfidence {
			return true
		}
		for _, n := range def.NotifyOn {
			if n == a.Choice {
				return true
			}
		}
	}
	return false
}

// firstLine trims a response body to its first non-empty line for surfacing
// in error messages. Multi-line bodies are common (HTML error pages) and the
// single line captures the human-meaningful prefix.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 200 {
				line = line[:200] + "..."
			}
			return line
		}
	}
	return ""
}
