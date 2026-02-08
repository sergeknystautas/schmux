// Package controlmode provides tmux control mode protocol parsing.
// Control mode allows programmatic interaction with tmux using a text-based protocol.
package controlmode

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// OutputEvent represents a %output notification from tmux control mode.
type OutputEvent struct {
	PaneID string // e.g., "%5" - the tmux pane ID
	Data   string // Unescaped content
}

// CommandResponse represents a response to a command.
type CommandResponse struct {
	CommandID int    // The CMD_ID from %begin/%end
	Success   bool   // true if %end, false if %error
	Content   string // The response content
}

// Event represents an asynchronous notification from tmux.
type Event struct {
	Type string   // e.g., "window-add", "session-changed"
	Args []string // Event arguments
}

// Parser parses tmux control mode protocol output.
type Parser struct {
	reader       *bufio.Reader
	connectionID string // For logging context

	// Channels for parsed data
	output    chan OutputEvent
	responses chan CommandResponse
	events    chan Event

	// Current command being parsed
	currentCmdID  int
	currentLines  []string
	inCommandResp bool

	// Signal when the first control mode protocol line is seen.
	// This indicates tmux has entered control mode and is ready for commands.
	controlModeReady chan struct{}
	controlModeOnce  sync.Once

	// Drop counters for monitoring channel saturation
	droppedOutputs   atomic.Int64
	droppedResponses atomic.Int64
	droppedEvents    atomic.Int64

	// Synchronization
	mu     sync.Mutex
	closed bool
}

// Guard line regex patterns
var (
	// %begin TIMESTAMP CMD_ID FLAGS
	beginRegex = regexp.MustCompile(`^%begin (\d+) (\d+) (\d+)$`)
	// %end TIMESTAMP CMD_ID FLAGS
	endRegex = regexp.MustCompile(`^%end (\d+) (\d+) (\d+)$`)
	// %error TIMESTAMP CMD_ID FLAGS
	errorRegex = regexp.MustCompile(`^%error (\d+) (\d+) (\d+)$`)
	// %output PANE_ID DATA
	outputRegex = regexp.MustCompile(`^%output (%\d+) (.*)$`)
)

// NewParser creates a new control mode parser.
// connID is an optional connection identifier for logging context.
func NewParser(r io.Reader, connID ...string) *Parser {
	id := ""
	if len(connID) > 0 {
		id = connID[0]
	}
	return &Parser{
		reader:           bufio.NewReader(r),
		connectionID:     id,
		output:           make(chan OutputEvent, 100),
		responses:        make(chan CommandResponse, 10000), // Large buffer to prevent blocking on slow networks
		events:           make(chan Event, 100),
		controlModeReady: make(chan struct{}),
	}
}

// Output returns the channel for output events.
func (p *Parser) Output() <-chan OutputEvent {
	return p.output
}

// Responses returns the channel for command responses.
func (p *Parser) Responses() <-chan CommandResponse {
	return p.responses
}

// Events returns the channel for async events.
func (p *Parser) Events() <-chan Event {
	return p.events
}

// ControlModeReady returns a channel that is closed when the first control mode
// protocol line (starting with %) is seen. This indicates tmux has entered
// control mode and is ready to accept commands.
func (p *Parser) ControlModeReady() <-chan struct{} {
	return p.controlModeReady
}

// Close closes the parser and its channels.
func (p *Parser) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.output)
		close(p.responses)
		close(p.events)
	}
}

// Run starts parsing lines from the reader.
// Blocks until EOF or error. Call in a goroutine.
func (p *Parser) Run() error {
	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if p.connectionID != "" {
					fmt.Printf("[controlmode:%s] parser EOF, closing\n", p.connectionID)
				}
				p.Close()
				return nil
			}
			if p.connectionID != "" {
				fmt.Printf("[controlmode:%s] read error: %v\n", p.connectionID, err)
			}
			p.Close()
			return fmt.Errorf("read error: %w", err)
		}

		// Remove trailing newline
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		if err := p.parseLine(line); err != nil {
			if p.connectionID != "" {
				fmt.Printf("[controlmode:%s] parse error: %v (line: %q)\n", p.connectionID, err, line)
			}
			return err
		}
	}
}

// parseLine handles a single line from tmux control mode.
func (p *Parser) parseLine(line string) error {
	// Signal control mode ready on the first protocol line
	if strings.HasPrefix(line, "%") {
		p.controlModeOnce.Do(func() {
			if p.connectionID != "" {
				fmt.Printf("[controlmode:%s] control mode protocol detected\n", p.connectionID)
			}
			close(p.controlModeReady)
		})
	}

	// Check for %begin
	if matches := beginRegex.FindStringSubmatch(line); matches != nil {
		cmdID, _ := strconv.Atoi(matches[2])
		p.inCommandResp = true
		p.currentCmdID = cmdID
		p.currentLines = nil
		return nil
	}

	// Check for %end
	if matches := endRegex.FindStringSubmatch(line); matches != nil {
		cmdID, _ := strconv.Atoi(matches[2])
		if p.inCommandResp && p.currentCmdID == cmdID {
			p.sendResponse(CommandResponse{
				CommandID: cmdID,
				Success:   true,
				Content:   strings.Join(p.currentLines, "\n"),
			})
			p.inCommandResp = false
		}
		return nil
	}

	// Check for %error
	if matches := errorRegex.FindStringSubmatch(line); matches != nil {
		cmdID, _ := strconv.Atoi(matches[2])
		if p.inCommandResp && p.currentCmdID == cmdID {
			p.sendResponse(CommandResponse{
				CommandID: cmdID,
				Success:   false,
				Content:   strings.Join(p.currentLines, "\n"),
			})
			p.inCommandResp = false
		}
		return nil
	}

	// Check for %output
	if matches := outputRegex.FindStringSubmatch(line); matches != nil {
		paneID := matches[1]
		data := UnescapeOutput(matches[2])
		p.sendOutput(OutputEvent{
			PaneID: paneID,
			Data:   data,
		})
		return nil
	}

	// If we're inside a command response, accumulate lines
	if p.inCommandResp {
		p.currentLines = append(p.currentLines, line)
		return nil
	}

	// Handle other notifications
	return p.parseNotification(line)
}

// parseNotification handles async notifications like %window-add, %session-changed, etc.
func (p *Parser) parseNotification(line string) error {
	if !strings.HasPrefix(line, "%") {
		// Not a notification, ignore (might be tmux startup messages)
		return nil
	}

	// Parse notification format: %notification-type args...
	parts := strings.SplitN(line, " ", 2)
	eventType := strings.TrimPrefix(parts[0], "%")

	var args []string
	if len(parts) > 1 {
		args = strings.Fields(parts[1])
	}

	p.sendEvent(Event{
		Type: eventType,
		Args: args,
	})

	return nil
}

// sendOutput sends an output event, dropping if closed or channel full.
func (p *Parser) sendOutput(e OutputEvent) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if !closed {
		select {
		case p.output <- e:
		default:
			// Drop if channel is full and log periodically
			dropped := p.droppedOutputs.Add(1)
			if dropped == 1 || dropped%100 == 0 {
				if p.connectionID != "" {
					fmt.Printf("[controlmode:%s] dropped %d output events (channel full)\n", p.connectionID, dropped)
				} else {
					fmt.Printf("[controlmode] dropped %d output events (channel full)\n", dropped)
				}
			}
		}
	}
}

// sendResponse sends a command response with timeout to prevent deadlock.
// Responses should never be dropped, but if the client has shut down or isn't draining,
// we must not block the parser forever.
func (p *Parser) sendResponse(r CommandResponse) {
	p.mu.Lock()
	closed := p.closed
	connID := p.connectionID
	p.mu.Unlock()

	if closed {
		return
	}

	// Check channel capacity and warn if approaching saturation
	const bufferSize = 10000
	const warningThreshold = 0.8
	currentLen := len(p.responses)
	if float64(currentLen) >= bufferSize*warningThreshold {
		if connID != "" {
			fmt.Printf("[controlmode:%s] WARNING: response buffer at %d/%d (%.1f%% full)\n",
				connID, currentLen, bufferSize, float64(currentLen)/bufferSize*100)
		} else {
			fmt.Printf("[controlmode] WARNING: response buffer at %d/%d (%.1f%% full)\n",
				currentLen, bufferSize, float64(currentLen)/bufferSize*100)
		}
	}

	// Try to send with timeout to prevent deadlock
	// Normal case: client is actively draining, this succeeds immediately
	// Abnormal case: client shut down but parser still running - log and continue
	// Configurable timeout (30s for slow networks, 5s was too aggressive)
	timeout := 30 * time.Second
	select {
	case p.responses <- r:
		// Successfully delivered
	case <-time.After(timeout):
		// Client isn't draining responses - this is a serious issue
		// Log loudly but don't block the parser forever
		if connID != "" {
			fmt.Printf("[controlmode:%s] WARNING: response channel blocked for %v (id=%d), client may have shut down\n", connID, timeout, r.CommandID)
		} else {
			fmt.Printf("[controlmode] WARNING: response channel blocked for %v (id=%d), client may have shut down\n", timeout, r.CommandID)
		}
		// Drop the response to prevent deadlock - client will timeout anyway
		dropped := p.droppedResponses.Add(1)
		if dropped == 1 || dropped%10 == 0 {
			fmt.Printf("[controlmode] WARNING: dropped %d response(s) due to blocked channel\n", dropped)
		}
	}
}

// sendEvent sends an event, dropping if closed or channel full.
func (p *Parser) sendEvent(e Event) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if !closed {
		select {
		case p.events <- e:
		default:
			// Drop if channel is full and log periodically
			dropped := p.droppedEvents.Add(1)
			if dropped == 1 || dropped%100 == 0 {
				fmt.Printf("[controlmode] dropped %d events (channel full)\n", dropped)
			}
		}
	}
}

// UnescapeOutput unescapes tmux control mode output.
// Characters < ASCII 32 and \ are escaped as octal: \NNN
// Common escapes:
//   - \\ -> \ (134)
//   - \015 -> CR (13)
//   - \012 -> LF (10)
//   - \033 -> ESC (27)
func UnescapeOutput(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			// Check for octal escape \NNN
			d1 := s[i+1]
			d2 := s[i+2]
			d3 := s[i+3]
			if isOctalDigit(d1) && isOctalDigit(d2) && isOctalDigit(d3) {
				val := (int(d1-'0') << 6) | (int(d2-'0') << 3) | int(d3-'0')
				result.WriteByte(byte(val))
				i += 3
				continue
			}
		}
		result.WriteByte(s[i])
	}

	return result.String()
}

// EscapeKeys escapes special characters for tmux send-keys.
// Used when sending input through control mode.
func EscapeKeys(s string) string {
	var result strings.Builder
	result.Grow(len(s) * 2)

	for _, c := range s {
		switch c {
		case '\\':
			result.WriteString("\\\\")
		case '\n':
			result.WriteString("Enter")
		case '\t':
			result.WriteString("Tab")
		case ' ':
			result.WriteString("Space")
		default:
			if c < 32 {
				// Control character
				result.WriteString(fmt.Sprintf("C-%c", 'a'+c-1))
			} else {
				result.WriteRune(c)
			}
		}
	}

	return result.String()
}

func isOctalDigit(b byte) bool {
	return b >= '0' && b <= '7'
}
