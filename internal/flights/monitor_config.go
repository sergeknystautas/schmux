package flights

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/sergeknystautas/schmux/internal/config"

	"gopkg.in/yaml.v3"
)

// errDecodeEOF is the sentinel yaml.Decoder returns when no further documents
// are available. It is the only "success" outcome of a second Decode call.
var errDecodeEOF = io.EOF

// MonitorChoice is one answer option for a Choice classifier.
type MonitorChoice struct {
	ID          string `yaml:"id" json:"id"`
	Description string `yaml:"description" json:"description"`
}

// MonitorClassifier is one Choice question and the rules that decide whether
// its answer wakes the pilot.
type MonitorClassifier struct {
	ID            string          `yaml:"id" json:"id"`
	Question      string          `yaml:"question" json:"question"`
	Choices       []MonitorChoice `yaml:"choices" json:"choices"`
	NotifyOn      []string        `yaml:"notify_on" json:"notify_on"`
	MinConfidence float64         `yaml:"min_confidence" json:"min_confidence"`
}

// MonitorRole groups classifiers by the session role they apply to.
type MonitorRole struct {
	Classifiers []MonitorClassifier `yaml:"classifiers" json:"classifiers"`
}

// MonitorConfig is the parsed classifier definitions in flight-monitor.yaml.
type MonitorConfig struct {
	Model string                 `yaml:"model" json:"model"`
	Roles map[string]MonitorRole `yaml:"roles" json:"roles"`
}

// MonitorAnswer is one Jev Choice answer, in the order the question asked it.
type MonitorAnswer struct {
	ID            string             `json:"id"`
	Choice        string             `json:"choice"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Classification is the structured answer for one response, in the same role
// order the YAML defined.
type Classification struct {
	Model   string          `json:"model"`
	Answers []MonitorAnswer `json:"answers"`
}

// monitorDefinitionsYAML is the flight workflow's classifier definitions. They
// are part of schmux, not per-user configuration: every installation asks the
// same questions, and nothing is written to or read from ~/.schmux.
//
//go:embed flight-monitor.yaml
var monitorDefinitionsYAML []byte

// MonitorReadinessError reports the local classification prerequisite: a
// configured TypeSafe API key. It does not contact Jev or expose credentials.
func MonitorReadinessError() string {
	key, err := config.GetTypesafeAPIKey()
	if err != nil {
		return fmt.Sprintf("flight-monitor: read TypeSafe credentials: %v", err)
	}
	if key == "" {
		return "Configure the TypeSafe API key in Settings > Flights > Pilot Supervision."
	}
	return ""
}

// parseMonitorConfig decodes and validates classifier definitions, preserving
// the YAML's classifier and choice order.
func parseMonitorConfig(data []byte) (MonitorConfig, error) {
	var cfg MonitorConfig
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return MonitorConfig{}, fmt.Errorf("flight-monitor: parse definitions: %w", err)
	}
	// Reject extra YAML documents. Decode returns io.EOF when there is no
	// further document; any other error or a successful second decode is a
	// multi-document file we explicitly do not support.
	var extra struct{}
	switch err := dec.Decode(&extra); {
	case err == nil:
		return MonitorConfig{}, errors.New("flight-monitor: definitions contain extra documents")
	case !errors.Is(err, errDecodeEOF):
		return MonitorConfig{}, fmt.Errorf("flight-monitor: definitions: extra document: %w", err)
	}
	if err := validateMonitorConfig(cfg); err != nil {
		return MonitorConfig{}, err
	}
	return cfg, nil
}

// validateMonitorConfig enforces the rules from the spec: model name present,
// the planner and executor roles defined, every classifier with a unique ID, at least two choices,
// finite thresholds in [0,1], notify_on entries that exist in the choices, and
// no extra top-level fields. parseMonitorConfig returns the parsed values in
// their original slice order.
func validateMonitorConfig(cfg MonitorConfig) error {
	if strings.TrimSpace(cfg.Model) == "" {
		return errors.New("flight-monitor: model is required")
	}
	for role, r := range cfg.Roles {
		if len(r.Classifiers) == 0 {
			return fmt.Errorf("flight-monitor: role %q has no classifiers", role)
		}
		seenIDs := map[string]bool{}
		for i, c := range r.Classifiers {
			if c.ID == "" {
				return fmt.Errorf("flight-monitor: role %q classifier %d has empty id", role, i)
			}
			if seenIDs[c.ID] {
				return fmt.Errorf("flight-monitor: role %q has duplicate classifier id %q", role, c.ID)
			}
			seenIDs[c.ID] = true
			if strings.TrimSpace(c.Question) == "" {
				return fmt.Errorf("flight-monitor: classifier %q has empty question", c.ID)
			}
			if len(c.Choices) < 2 {
				return fmt.Errorf("flight-monitor: classifier %q needs at least two choices", c.ID)
			}
			choiceIDs := map[string]bool{}
			for j, ch := range c.Choices {
				if ch.ID == "" {
					return fmt.Errorf("flight-monitor: classifier %q choice %d has empty id", c.ID, j)
				}
				if choiceIDs[ch.ID] {
					return fmt.Errorf("flight-monitor: classifier %q has duplicate choice id %q", c.ID, ch.ID)
				}
				choiceIDs[ch.ID] = true
			}
			if math.IsNaN(c.MinConfidence) || math.IsInf(c.MinConfidence, 0) ||
				c.MinConfidence < 0 || c.MinConfidence > 1 {
				return fmt.Errorf("flight-monitor: classifier %q has invalid min_confidence %v", c.ID, c.MinConfidence)
			}
			for j, n := range c.NotifyOn {
				if !choiceIDs[n] {
					return fmt.Errorf("flight-monitor: classifier %q notify_on[%d]=%q is not a choice", c.ID, j, n)
				}
			}
		}
	}
	for _, required := range []SessionRole{RolePlanner, RoleExecutor} {
		if _, ok := cfg.Roles[string(required)]; !ok {
			return fmt.Errorf("flight-monitor: role %q is required", string(required))
		}
	}
	return nil
}
