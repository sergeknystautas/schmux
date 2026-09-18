package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestReadCodexActiveHistoryUsesLatestCompactedReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	records := []any{
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{}}},
		map[string]any{"type": "compacted", "payload": map[string]any{"replacement_history": []any{
			map[string]any{"type": "message", "role": "developer", "content": []any{}},
		}}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": "after_compaction"}},
	}
	var contents []byte
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents, line...)
		contents = append(contents, '\n')
	}
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}

	history, err := readCodexActiveHistory(path)
	if err != nil {
		t.Fatalf("readCodexActiveHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2; history=%s", len(history), history)
	}
	if got := responseItemType(t, history[0]); got != "message" {
		t.Errorf("history[0] type = %q, want message", got)
	}
	if got := responseItemType(t, history[1]); got != "function_call" {
		t.Errorf("history[1] type = %q, want function_call", got)
	}
}

func TestFilterCodexHistoryUsesDestinationCompatibility(t *testing.T) {
	message := json.RawMessage(`{"type":"message","role":"user","content":[]}`)
	foreignReasoning := json.RawMessage(`{"type":"reasoning","id":"foreign","summary":[],"content":[{"type":"reasoning_text","text":"private"}],"encrypted_content":null}`)
	openAIReasoning := json.RawMessage(`{"type":"reasoning","id":"openai","summary":[],"content":[],"encrypted_content":"ciphertext"}`)
	history := []json.RawMessage{message, foreignReasoning, openAIReasoning}

	tests := []struct {
		provider    string
		wantTypes   []string
		wantOmitted int
	}{
		{provider: "openai", wantTypes: []string{"message", "reasoning"}, wantOmitted: 1},
		{provider: "zai", wantTypes: []string{"message", "reasoning", "reasoning"}, wantOmitted: 0},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			got, omitted, err := filterCodexHistory(test.provider, history)
			if err != nil {
				t.Fatalf("filterCodexHistory: %v", err)
			}
			if omitted != test.wantOmitted {
				t.Errorf("omitted = %d, want %d", omitted, test.wantOmitted)
			}
			if len(got) != len(test.wantTypes) {
				t.Fatalf("history length = %d, want %d; history=%s", len(got), len(test.wantTypes), got)
			}
			for index, wantType := range test.wantTypes {
				if gotType := responseItemType(t, got[index]); gotType != wantType {
					t.Errorf("history[%d] type = %q, want %q", index, gotType, wantType)
				}
			}
		})
	}
}

func TestPrepareCodexRestartHistoryCreatesFilteredContinuation(t *testing.T) {
	dir := t.TempDir()
	rolloutPath := filepath.Join(dir, "source.jsonl")
	source := []byte("{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[]}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"reasoning\",\"id\":\"foreign\",\"summary\":[],\"content\":[{\"type\":\"reasoning_text\",\"text\":\"private\"}],\"encrypted_content\":null}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"kept\",\"arguments\":\"{}\",\"call_id\":\"call-1\"}}\n")
	if err := os.WriteFile(rolloutPath, source, 0600); err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(dir, "resume-request.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(dir, "codex-test-helper")
	wrapper := fmt.Sprintf("#!/bin/sh\nexec %s -test.run=TestRestartHistoryHelperProcess -- \"$@\"\n", strconv.Quote(executable))
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}

	preparedID, omitted, err := prepareCodexRestartHistory(context.Background(), restartHistoryOptions{
		Target: ResolvedTarget{
			Command:  wrapperPath,
			ToolName: "codex",
			Env: map[string]string{
				"SCHMUX_RESTART_TEST_HELPER":   "1",
				"SCHMUX_RESTART_TEST_PROVIDER": "openai",
				"SCHMUX_RESTART_TEST_ROLLOUT":  rolloutPath,
				"SCHMUX_RESTART_TEST_CAPTURE":  capturePath,
			},
		},
		ModelValue: "gpt-test",
		ResumeID:   "source-thread",
		Cwd:        dir,
	})
	if err != nil {
		t.Fatalf("prepareCodexRestartHistory: %v", err)
	}
	if preparedID != "prepared-thread" || omitted != 1 {
		t.Fatalf("prepared result = (%q, %d), want (prepared-thread, 1)", preparedID, omitted)
	}
	after, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(source) {
		t.Fatal("source rollout changed")
	}

	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured resume request: %v", err)
	}
	var request struct {
		History []json.RawMessage `json:"history"`
		Model   string            `json:"model"`
	}
	if err := json.Unmarshal(captured, &request); err != nil {
		t.Fatalf("decode captured resume request: %v", err)
	}
	if request.Model != "gpt-test" {
		t.Errorf("model = %q, want gpt-test", request.Model)
	}
	if len(request.History) != 2 {
		t.Fatalf("prepared history length = %d, want 2; history=%s", len(request.History), request.History)
	}
	if first, second := responseItemType(t, request.History[0]), responseItemType(t, request.History[1]); first != "message" || second != "function_call" {
		t.Errorf("prepared history types = [%s %s], want [message function_call]", first, second)
	}
}

func TestRestartHistoryHelperProcess(t *testing.T) {
	if os.Getenv("SCHMUX_RESTART_TEST_HELPER") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			return
		}
		if request.ID == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{}
		case "config/read":
			result = map[string]any{"config": map[string]any{"model_provider": os.Getenv("SCHMUX_RESTART_TEST_PROVIDER")}, "origins": map[string]any{}}
		case "thread/read":
			result = map[string]any{"thread": map[string]any{"path": os.Getenv("SCHMUX_RESTART_TEST_ROLLOUT")}}
		case "thread/resume":
			if err := os.WriteFile(os.Getenv("SCHMUX_RESTART_TEST_CAPTURE"), request.Params, 0600); err != nil {
				os.Exit(2)
			}
			result = map[string]any{"thread": map[string]any{"id": "prepared-thread"}}
		default:
			_ = encoder.Encode(map[string]any{"id": request.ID, "error": map[string]any{"message": "unexpected method " + request.Method}})
			continue
		}
		if err := encoder.Encode(map[string]any{"id": request.ID, "result": result}); err != nil {
			return
		}
	}
}

func responseItemType(t *testing.T, item json.RawMessage) string {
	t.Helper()
	var decoded struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(item, &decoded); err != nil {
		t.Fatalf("decode response item: %v; item=%s", err, item)
	}
	return decoded.Type
}
