package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/detect"
)

// PrepareRestartResume checks the saved harness history against the
// destination selected for a Restart. Most harnesses can resume the existing
// conversation directly. Harnesses with a history preparer may instead return
// a new continuation id whose model-visible history omits destination-
// incompatible items; the source conversation is never modified.
func (m *Manager) PrepareRestartResume(ctx context.Context, targetName, resumeID, cwd string) (string, error) {
	target, err := m.ResolveTarget(ctx, targetName)
	if err != nil {
		return "", err
	}
	adapter := detect.GetAdapter(target.ToolName)
	if adapter == nil {
		return resumeID, nil
	}
	preparer := restartHistoryPreparers[adapter.ChatProtocol()]
	if preparer == nil {
		return resumeID, nil
	}
	modelValue := ""
	if target.Model != nil {
		if runner, ok := target.Model.RunnerFor(target.ToolName); ok {
			modelValue = runner.ModelValue
		}
	}
	preparedID, omitted, err := preparer(ctx, restartHistoryOptions{
		Target:     target,
		ModelValue: modelValue,
		ResumeID:   resumeID,
		Cwd:        cwd,
	})
	if err != nil {
		return "", err
	}
	if omitted > 0 {
		m.logger.Info("prepared destination-compatible restart history",
			"harness", target.ToolName,
			"source_resume_id", resumeID,
			"prepared_resume_id", preparedID,
			"omitted_items", omitted)
	}
	return preparedID, nil
}

type restartHistoryOptions struct {
	Target     ResolvedTarget
	ModelValue string
	ResumeID   string
	Cwd        string
}

type restartHistoryPreparer func(context.Context, restartHistoryOptions) (resumeID string, omitted int, err error)

var restartHistoryPreparers = map[string]restartHistoryPreparer{
	chat.ProtocolCodex: prepareCodexRestartHistory,
}

type codexRPCClient struct {
	encode *json.Encoder
	decode *json.Decoder
	nextID int
}

type codexRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (c *codexRPCClient) notify(method string, params any) error {
	notification := map[string]any{"method": method}
	if params != nil {
		notification["params"] = params
	}
	return c.encode.Encode(notification)
}

func (c *codexRPCClient) call(method string, params any, result any) error {
	c.nextID++
	id := c.nextID
	if err := c.encode.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var response codexRPCResponse
		if err := c.decode.Decode(&response); err != nil {
			return err
		}
		if response.ID != id {
			continue
		}
		if len(response.Error) > 0 && !bytes.Equal(response.Error, []byte("null")) {
			return fmt.Errorf("%s: %s", method, response.Error)
		}
		if result == nil || len(response.Result) == 0 {
			return nil
		}
		return json.Unmarshal(response.Result, result)
	}
}

func prepareCodexRestartHistory(ctx context.Context, opts restartHistoryOptions) (string, int, error) {
	adapter := detect.GetAdapter(opts.Target.ToolName)
	if adapter == nil {
		return "", 0, fmt.Errorf("restart history: unknown harness %q", opts.Target.ToolName)
	}
	parts := append(strings.Fields(opts.Target.Command), adapter.ChatArgs(nil, false, "")...)
	parts = append(parts, opts.Target.Args...)
	if len(parts) == 0 {
		return "", 0, errors.New("restart history: empty harness command")
	}

	command := exec.CommandContext(ctx, parts[0], parts[1:]...)
	command.Dir = opts.Cwd
	command.Env = restartProcessEnv(opts.Target.Env)
	stdin, err := command.StdinPipe()
	if err != nil {
		return "", 0, fmt.Errorf("restart history: stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", 0, fmt.Errorf("restart history: stdout: %w", err)
	}
	var stderr synchronizedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return "", 0, fmt.Errorf("restart history: start %s: %w", opts.Target.ToolName, err)
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	client := codexRPCClient{encode: json.NewEncoder(stdin), decode: json.NewDecoder(stdout)}
	var initialized json.RawMessage
	if err := client.call("initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "schmux-restart-history", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &initialized); err != nil {
		return "", 0, codexRestartHistoryError(err, stderr.String())
	}
	if err := client.notify("initialized", nil); err != nil {
		return "", 0, codexRestartHistoryError(err, stderr.String())
	}

	var configResult struct {
		Config struct {
			ModelProvider string `json:"model_provider"`
		} `json:"config"`
	}
	if err := client.call("config/read", map[string]any{"cwd": opts.Cwd, "includeLayers": false}, &configResult); err != nil {
		return "", 0, codexRestartHistoryError(err, stderr.String())
	}
	provider := configResult.Config.ModelProvider
	if provider == "" {
		provider = "openai"
	}

	var readResult struct {
		Thread struct {
			Path string `json:"path"`
		} `json:"thread"`
	}
	if err := client.call("thread/read", map[string]any{"threadId": opts.ResumeID, "includeTurns": false}, &readResult); err != nil {
		return "", 0, codexRestartHistoryError(err, stderr.String())
	}
	if readResult.Thread.Path == "" {
		return "", 0, errors.New("restart history: Codex returned no rollout path")
	}
	history, err := readCodexActiveHistory(readResult.Thread.Path)
	if err != nil {
		return "", 0, err
	}
	compatible, omitted, err := filterCodexHistory(provider, history)
	if err != nil {
		return "", 0, err
	}
	if omitted == 0 {
		return opts.ResumeID, 0, nil
	}

	var resumeResult struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	resumeParams := map[string]any{
		"threadId":       "schmux-destination-compatible-history",
		"history":        compatible,
		"cwd":            opts.Cwd,
		"excludeTurns":   true,
		"approvalPolicy": "never",
		"sandbox":        "read-only",
	}
	if opts.ModelValue != "" {
		resumeParams["model"] = opts.ModelValue
	}
	if err := client.call("thread/resume", resumeParams, &resumeResult); err != nil {
		return "", 0, codexRestartHistoryError(err, stderr.String())
	}
	if resumeResult.Thread.ID == "" {
		return "", 0, errors.New("restart history: Codex returned no continuation id")
	}
	return resumeResult.Thread.ID, omitted, nil
}

func codexRestartHistoryError(err error, stderr string) error {
	const maxStderr = 4096
	if len(stderr) > maxStderr {
		stderr = stderr[len(stderr)-maxStderr:]
	}
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("restart history: %w", err)
	}
	return fmt.Errorf("restart history: %w (codex stderr: %s)", err, stderr)
}

func restartProcessEnv(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func readCodexActiveHistory(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("restart history: open Codex rollout: %w", err)
	}
	defer file.Close()

	var history []json.RawMessage
	reader := bufio.NewReader(file)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNumber++
			var record struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(line, &record); err != nil {
				return nil, fmt.Errorf("restart history: parse Codex rollout line %d: %w", lineNumber, err)
			}
			switch record.Type {
			case "response_item":
				history = append(history, append(json.RawMessage(nil), record.Payload...))
			case "compacted":
				var compacted struct {
					ReplacementHistory []json.RawMessage `json:"replacement_history"`
				}
				if err := json.Unmarshal(record.Payload, &compacted); err != nil {
					return nil, fmt.Errorf("restart history: parse compacted rollout line %d: %w", lineNumber, err)
				}
				history = append([]json.RawMessage(nil), compacted.ReplacementHistory...)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("restart history: read Codex rollout: %w", readErr)
		}
	}
	return history, nil
}

func filterCodexHistory(provider string, history []json.RawMessage) ([]json.RawMessage, int, error) {
	compatible := make([]json.RawMessage, 0, len(history))
	omitted := 0
	for index, item := range history {
		keep, err := codexHistoryItemCompatible(provider, item)
		if err != nil {
			return nil, 0, fmt.Errorf("restart history: inspect item %d: %w", index, err)
		}
		if !keep {
			omitted++
			continue
		}
		compatible = append(compatible, item)
	}
	return compatible, omitted, nil
}

func codexHistoryItemCompatible(provider string, item json.RawMessage) (bool, error) {
	if provider != "openai" {
		return true, nil
	}
	var responseItem struct {
		Type    string            `json:"type"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(item, &responseItem); err != nil {
		return false, err
	}
	// Third-party Responses-compatible providers can emit plaintext reasoning
	// content with a provider-owned item id. OpenAI accepts neither that content
	// shape nor a reference to the unpersisted foreign item. The complete item
	// therefore stays in the source thread and is absent from this continuation.
	return responseItem.Type != "reasoning" || len(responseItem.Content) == 0, nil
}
