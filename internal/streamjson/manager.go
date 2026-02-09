package streamjson

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ClientSender can receive JSON messages from the stream-json process.
type ClientSender interface {
	SendJSON(data []byte) error
}

// StreamSession manages a single Claude Code stream-json subprocess.
type StreamSession struct {
	SessionID string
	Cmd       *exec.Cmd
	Stdin     io.WriteCloser
	stdinMu   sync.Mutex // serialize writes to stdin

	Messages   []Message
	MessagesMu sync.RWMutex

	Clients   map[ClientSender]bool
	ClientsMu sync.RWMutex

	Cancel context.CancelFunc
	Done   chan struct{}
}

// Manager manages stream-json subprocess sessions.
type Manager struct {
	sessions map[string]*StreamSession
	mu       sync.RWMutex
}

// NewManager creates a new stream-json session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*StreamSession),
	}
}

// Start launches a Claude Code stream-json subprocess.
func (m *Manager) Start(sessionID, workDir, command string, args []string, env []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[sessionID]; exists {
		return 0, fmt.Errorf("stream-json session already exists: %s", sessionID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir
	cmd.Env = env
	// Set process group so we can kill the entire group on dispose
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return 0, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return 0, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Merge stderr into stdout so we capture everything
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return 0, fmt.Errorf("failed to start stream-json process: %w", err)
	}

	pid := cmd.Process.Pid

	sess := &StreamSession{
		SessionID: sessionID,
		Cmd:       cmd,
		Stdin:     stdin,
		Messages:  make([]Message, 0),
		Clients:   make(map[ClientSender]bool),
		Cancel:    cancel,
		Done:      make(chan struct{}),
	}

	m.sessions[sessionID] = sess

	// Start stdout reader goroutine
	go m.readOutput(sess, stdout)

	// Start process waiter goroutine
	go m.waitProcess(sess)

	fmt.Printf("[streamjson] started session %s (pid=%d)\n", sessionID, pid)
	return pid, nil
}

// readOutput reads NDJSON lines from stdout and broadcasts to clients.
func (m *Manager) readOutput(sess *StreamSession, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	// Allow large lines (up to 10MB) for tool results
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse the type field
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			// Not valid JSON, skip
			continue
		}

		// Make a copy of the line for storage
		rawCopy := make(json.RawMessage, len(line))
		copy(rawCopy, line)

		msg := Message{
			Type:      envelope.Type,
			Raw:       rawCopy,
			Timestamp: time.Now(),
		}

		// Append to message history
		sess.MessagesMu.Lock()
		sess.Messages = append(sess.Messages, msg)
		// Cap at 1000 messages to prevent memory growth
		if len(sess.Messages) > 1000 {
			sess.Messages = sess.Messages[len(sess.Messages)-1000:]
		}
		sess.MessagesMu.Unlock()

		// Broadcast to all connected clients
		wsMsg, _ := json.Marshal(map[string]interface{}{
			"type":    "message",
			"message": json.RawMessage(rawCopy),
		})

		sess.ClientsMu.RLock()
		for client := range sess.Clients {
			if err := client.SendJSON(wsMsg); err != nil {
				// Client will be cleaned up by its own goroutine
			}
		}
		sess.ClientsMu.RUnlock()
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("[streamjson] scanner error for session %s: %v\n", sess.SessionID, err)
	}
}

// waitProcess waits for the subprocess to exit and notifies clients.
func (m *Manager) waitProcess(sess *StreamSession) {
	_ = sess.Cmd.Wait()
	close(sess.Done)

	// Notify all clients that the session has stopped
	statusMsg, _ := json.Marshal(map[string]string{
		"type":   "status",
		"status": "stopped",
	})

	sess.ClientsMu.RLock()
	for client := range sess.Clients {
		client.SendJSON(statusMsg)
	}
	sess.ClientsMu.RUnlock()

	fmt.Printf("[streamjson] session %s process exited\n", sess.SessionID)
}

// SendUserMessage sends a user message to the subprocess stdin.
func (m *Manager) SendUserMessage(sessionID, content string) error {
	m.mu.RLock()
	sess, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("stream-json session not found: %s", sessionID)
	}

	msg := NewUserInputMessage(content)
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal user message: %w", err)
	}

	sess.stdinMu.Lock()
	defer sess.stdinMu.Unlock()

	// Write JSON line followed by newline
	if _, err := sess.Stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// SendPermissionResponse sends a permission response to the subprocess stdin.
func (m *Manager) SendPermissionResponse(sessionID, requestID string, approved bool) error {
	m.mu.RLock()
	sess, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("stream-json session not found: %s", sessionID)
	}

	msg := NewPermissionResponse(requestID, approved)
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal permission response: %w", err)
	}

	sess.stdinMu.Lock()
	defer sess.stdinMu.Unlock()

	if _, err := sess.Stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// GetMessages returns the message history for a session.
func (m *Manager) GetMessages(sessionID string) ([]Message, error) {
	m.mu.RLock()
	sess, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("stream-json session not found: %s", sessionID)
	}

	sess.MessagesMu.RLock()
	defer sess.MessagesMu.RUnlock()

	msgs := make([]Message, len(sess.Messages))
	copy(msgs, sess.Messages)
	return msgs, nil
}

// RegisterClient adds a WebSocket client to receive messages for a session.
func (m *Manager) RegisterClient(sessionID string, client ClientSender) error {
	m.mu.RLock()
	sess, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("stream-json session not found: %s", sessionID)
	}

	sess.ClientsMu.Lock()
	sess.Clients[client] = true
	sess.ClientsMu.Unlock()
	return nil
}

// UnregisterClient removes a WebSocket client from a session.
func (m *Manager) UnregisterClient(sessionID string, client ClientSender) {
	m.mu.RLock()
	sess, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return
	}

	sess.ClientsMu.Lock()
	delete(sess.Clients, client)
	sess.ClientsMu.Unlock()
}

// Stop terminates a stream-json session.
func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	sess, exists := m.sessions[sessionID]
	if !exists {
		m.mu.Unlock()
		return nil // Already stopped
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	// Close stdin first to signal the process
	sess.Stdin.Close()

	// Send SIGTERM to process group
	if sess.Cmd.Process != nil {
		pid := sess.Cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGTERM)

		// Wait briefly for graceful shutdown
		select {
		case <-sess.Done:
			// Process exited
		case <-time.After(3 * time.Second):
			// Force kill
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}

	// Cancel context
	sess.Cancel()

	fmt.Printf("[streamjson] stopped session %s\n", sessionID)
	return nil
}

// IsRunning checks if a stream-json session process is still running.
func (m *Manager) IsRunning(sessionID string) bool {
	m.mu.RLock()
	sess, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return false
	}

	select {
	case <-sess.Done:
		return false
	default:
		return true
	}
}

// GetSession returns a stream session by ID.
func (m *Manager) GetSession(sessionID string) (*StreamSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, exists := m.sessions[sessionID]
	return sess, exists
}
