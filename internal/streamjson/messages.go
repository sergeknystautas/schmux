package streamjson

import (
	"encoding/json"
	"time"
)

// Message represents a parsed NDJSON message from Claude Code's stream-json output.
// We store the raw JSON to forward to clients without losing fields.
type Message struct {
	Type      string          `json:"type"`
	Raw       json.RawMessage `json:"-"` // The full raw JSON line
	Timestamp time.Time       `json:"timestamp"`
}

// UserInputMessage is the JSON sent to Claude Code stdin for user messages.
type UserInputMessage struct {
	Type    string           `json:"type"`
	Message UserInputContent `json:"message"`
}

// UserInputContent is the content of a user input message.
type UserInputContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NewUserInputMessage creates a user message to send to Claude Code stdin.
func NewUserInputMessage(content string) UserInputMessage {
	return UserInputMessage{
		Type: "user",
		Message: UserInputContent{
			Role:    "user",
			Content: content,
		},
	}
}

// PermissionResponse is the JSON sent to Claude Code stdin for permission responses.
type PermissionResponse struct {
	Type      string                   `json:"type"`
	RequestID string                   `json:"request_id"`
	Result    PermissionResponseResult `json:"result"`
}

// PermissionResponseResult is the result of a permission response.
type PermissionResponseResult struct {
	Approved bool `json:"approved"`
}

// NewPermissionResponse creates a permission response to send to Claude Code stdin.
func NewPermissionResponse(requestID string, approved bool) PermissionResponse {
	return PermissionResponse{
		Type:      "permission_response",
		RequestID: requestID,
		Result: PermissionResponseResult{
			Approved: approved,
		},
	}
}
