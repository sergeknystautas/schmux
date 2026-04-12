package telemetry

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// CommandTelemetry sends events to an external command via stdin.
// Each Track call spawns the command once and writes typed JSON to its stdin.
// The JSON format uses int/normal/double buckets to categorize properties by type.
type CommandTelemetry struct {
	command     string
	installID   string
	logger      *log.Logger
	lastFailLog time.Time
	failLogMu   sync.Mutex
}

// NewCommandTelemetry creates a CommandTelemetry that execs the given shell command.
func NewCommandTelemetry(command, installID string, logger *log.Logger) *CommandTelemetry {
	return &CommandTelemetry{
		command:   command,
		installID: installID,
		logger:    logger,
	}
}

// Track sends an event to the configured command as typed JSON on stdin.
func (c *CommandTelemetry) Track(event string, properties map[string]any) {
	intBucket := map[string]any{
		"time": time.Now().Unix(),
	}
	normalBucket := map[string]any{
		"event":           event,
		"installation_id": c.installID,
	}
	doubleBucket := map[string]any{}

	for k, v := range properties {
		switch val := v.(type) {
		case string:
			normalBucket[k] = val
		case int:
			intBucket[k] = val
		case int64:
			intBucket[k] = val
		case float64:
			doubleBucket[k] = val
		case bool:
			if val {
				intBucket[k] = 1
			} else {
				intBucket[k] = 0
			}
		default:
			normalBucket[k] = fmt.Sprintf("%v", v)
		}
	}

	payload := map[string]any{
		"int":    intBucket,
		"normal": normalBucket,
	}
	if len(doubleBucket) > 0 {
		payload["double"] = doubleBucket
	}

	data, err := json.Marshal(payload)
	if err != nil {
		c.logFailure("failed to marshal telemetry event", "err", err)
		return
	}

	cmd := exec.Command("sh", "-c", c.command)
	cmd.Stdin = strings.NewReader(string(data))

	if err := cmd.Start(); err != nil {
		c.logFailure("failed to start telemetry command", "err", err)
		return
	}
	go cmd.Wait() // reap child process without blocking
}

// Shutdown is a no-op for CommandTelemetry -- there is no queue to flush.
func (c *CommandTelemetry) Shutdown() {}

// logFailure logs a failure message, rate-limited to once per minute.
func (c *CommandTelemetry) logFailure(msg string, keyvals ...interface{}) {
	c.failLogMu.Lock()
	defer c.failLogMu.Unlock()

	if time.Since(c.lastFailLog) < time.Minute {
		return
	}
	c.lastFailLog = time.Now()
	if c.logger != nil {
		c.logger.Error(msg, keyvals...)
	}
}
