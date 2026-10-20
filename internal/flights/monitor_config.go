package flights

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
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

// MonitorClassifier is one editable Choice question and the rules that decide
// whether its answer wakes the pilot.
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

// MonitorConfig is the parsed contents of ~/.schmux/flight-monitor.yaml.
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

//go:embed flight-monitor.yaml
var monitorTemplateYAML []byte

// MonitorReadinessError reports local credentials and classifier configuration prerequisites.
// It does not contact Jev or expose credentials.
func MonitorReadinessError(path string) string {
	key, err := config.GetTypesafeAPIKey()
	if err != nil {
		return fmt.Sprintf("flight-monitor: read TypeSafe credentials: %v", err)
	}
	if key == "" {
		return "Configure the TypeSafe API key in Settings > Flights > Pilot Supervision."
	}
	cfg, err := loadMonitorConfig(path)
	if err != nil {
		return err.Error()
	}
	if len(cfg.Roles[string(RolePlanner)].Classifiers) == 0 {
		return "flight-monitor: no classifier definitions for planner"
	}
	return ""
}

// loadMonitorConfig reads, validates, and returns the YAML at path. The file
// must exist; callers that want schmux to create the template on first run use
// ensureMonitorConfig instead.
func loadMonitorConfig(path string) (MonitorConfig, error) {
	var cfg MonitorConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return MonitorConfig{}, fmt.Errorf("flight-monitor: read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return MonitorConfig{}, fmt.Errorf("flight-monitor: parse %s: %w", path, err)
	}
	// Reject extra YAML documents. Decode returns io.EOF when there is no
	// further document; any other error or a successful second decode is a
	// multi-document file we explicitly do not support.
	var extra struct{}
	switch err := dec.Decode(&extra); {
	case err == nil:
		return MonitorConfig{}, fmt.Errorf("flight-monitor: %s contains extra documents", path)
	case !errors.Is(err, errDecodeEOF):
		return MonitorConfig{}, fmt.Errorf("flight-monitor: %s: extra document: %w", path, err)
	}
	if err := validateMonitorConfig(cfg); err != nil {
		return MonitorConfig{}, err
	}
	return cfg, nil
}

// ensureMonitorConfig writes the embedded template at path if the file is
// missing. Existing files are never overwritten. Owner-only permissions are
// enforced so a multi-user system cannot read another user's classifier file.
func ensureMonitorConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("flight-monitor: stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("flight-monitor: mkdir %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("flight-monitor: create %s: %w", path, err)
	}
	_, writeErr := file.Write(monitorTemplateYAML)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("flight-monitor: write %s: %w", path, err)
	}
	return nil
}

// validateMonitorConfig enforces the rules from the spec: model name present,
// at least one role, every classifier with a unique ID, at least two choices,
// finite thresholds in [0,1], notify_on entries that exist in the choices, and
// no extra top-level fields. Order is preserved by walking the raw YAML after
// decode if the caller asks for it; loadMonitorConfig returns the parsed
// values in their original slice order.
func validateMonitorConfig(cfg MonitorConfig) error {
	if strings.TrimSpace(cfg.Model) == "" {
		return errors.New("flight-monitor: model is required")
	}
	if len(cfg.Roles) == 0 {
		return errors.New("flight-monitor: at least one role is required")
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
	return nil
}
