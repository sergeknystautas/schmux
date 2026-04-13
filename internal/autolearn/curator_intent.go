//go:build !noautolearn

package autolearn

import (
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelAutolearnIntent, IntentCuratorResponse{})
}

// IntentCuratorResponse is the expected JSON output from the intent curator LLM.
type IntentCuratorResponse struct {
	NewLearnings     []Learning        `json:"new_learnings"`
	UpdatedLearnings []Learning        `json:"updated_learnings"`
	DiscardedSignals map[string]string `json:"discarded_signals"`
}
