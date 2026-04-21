//go:build !norepofeed

package repofeed

import (
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelRepofeedIntent, IntentSummary{})
}

// IntentSummary is the parsed LLM response for workspace intent summarization.
// The prompt instructs the model to return {"summary": "one sentence"} with a
// character limit; callers truncate further if needed.
type IntentSummary struct {
	Summary string   `json:"summary" required:"true"`
	_       struct{} `additionalProperties:"false"`
}
