package commitmessage

import (
	"github.com/sergeknystautas/schmux/internal/schema"
)

func init() {
	schema.Register(schema.LabelCommitMessage, Result{})
}

// Result is the structured output for commit message generation.
type Result struct {
	Message string   `json:"message" required:"true"`
	_       struct{} `additionalProperties:"false"`
}
