package flights

import "strings"

const (
	beginPrompt = "----- BEGIN ORIGINAL PROMPT -----"
	endPrompt   = "----- END ORIGINAL PROMPT -----"
)

// promptBlock wraps the original flight prompt, byte for byte, in delimiters.
func promptBlock(prompt string) string {
	return beginPrompt + "\n" + prompt + "\n" + endPrompt
}

// PlannerPrompt composes the planner session's first message: act as planner
// and do not implement, use the brainstorming skill, the original prompt, and
// the absolute path of the plan to write.
func PlannerPrompt(prompt, artifactPath string) string {
	var b strings.Builder
	b.WriteString("You are the planner for this work. Plan it; do not implement it.\n\n")
	b.WriteString("Use the superpowers:brainstorming workflow to plan this work.\n\n")
	b.WriteString("The original request is reproduced verbatim between the delimiters below.\n\n")
	b.WriteString(promptBlock(prompt))
	b.WriteString("\n\nWrite the final implementation plan to this absolute path, creating it if needed:\n")
	b.WriteString(artifactPath)
	b.WriteString("\n")
	return b.String()
}

// ExecutorPrompt composes the executor session's first message: implement the
// plan, the original prompt, and the absolute path of the plan to read first.
func ExecutorPrompt(prompt, artifactPath string) string {
	var b strings.Builder
	b.WriteString("You are the executor for this work. Implement the plan in the current workspace.\n\n")
	b.WriteString("The original request is reproduced verbatim between the delimiters below.\n\n")
	b.WriteString(promptBlock(prompt))
	b.WriteString("\n\nThe plan is at this absolute path. Read that file before editing anything:\n")
	b.WriteString(artifactPath)
	b.WriteString("\n")
	return b.String()
}
