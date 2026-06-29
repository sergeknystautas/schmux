package contracts

// OneshotLogRecord is one line of ~/.schmux/logs/oneshot.jsonl: a single
// non-interactive oneshot LLM call and its result. Metadata only — no prompt
// body is persisted (PromptChars holds len(prompt) instead).
type OneshotLogRecord struct {
	TS          string `json:"ts"`                     // RFC3339 start time
	Type        string `json:"type"`                   // schema label, e.g. "commit-message"
	Transport   string `json:"transport,omitempty"`    // "cli" or "api"
	Model       string `json:"model,omitempty"`        // model id (targetName, ::api stripped)
	Workspace   string `json:"workspace,omitempty"`    // basename of the call's dir; "" when none
	PromptChars int    `json:"prompt_chars,omitempty"` // len(prompt)
	ElapsedMS   int64  `json:"elapsed_ms,omitempty"`
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"` // error string on failure
}
