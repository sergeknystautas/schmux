package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// termStyle provides terminal styling helpers with automatic color detection
type termStyle struct {
	useColors bool
}

func newTermStyle() *termStyle {
	return &termStyle{
		useColors: term.IsTerminal(int(os.Stdout.Fd())),
	}
}

func (t *termStyle) colorize(code, text string) string {
	if !t.useColors {
		return text
	}
	return code + text + ansiReset
}

// Header prints a section header with divider bars
func (t *termStyle) Header(title string) {
	bar := strings.Repeat("━", 72)
	fmt.Println()
	fmt.Println(t.colorize(ansiCyan, bar))
	fmt.Println(t.colorize(ansiBold+ansiCyan, "  "+title))
	fmt.Println(t.colorize(ansiCyan, bar))
	fmt.Println()
}

// SubHeader prints a smaller section header (no top bar)
func (t *termStyle) SubHeader(title string) {
	bar := strings.Repeat("─", 72)
	fmt.Println()
	fmt.Println(t.colorize(ansiCyan, bar))
	fmt.Println(t.colorize(ansiBold+ansiCyan, "  "+title))
	fmt.Println(t.colorize(ansiCyan, bar))
	fmt.Println()
}

// Success prints a success message with green checkmark
func (t *termStyle) Success(msg string) {
	fmt.Println(t.colorize(ansiGreen, "✓ "+msg))
}

// Warn prints a warning message with yellow warning symbol
func (t *termStyle) Warn(msg string) {
	fmt.Println(t.colorize(ansiYellow, "⚠ "+msg))
}

// Error prints an error message with red X
func (t *termStyle) Error(msg string) {
	fmt.Println(t.colorize(ansiRed, "✗ "+msg))
}

// Dim returns dimmed text
func (t *termStyle) Dim(text string) string {
	return t.colorize(ansiDim, text)
}

// Bold returns bold text
func (t *termStyle) Bold(text string) string {
	return t.colorize(ansiBold, text)
}

// Cyan returns cyan text (for URLs, commands, paths)
func (t *termStyle) Cyan(text string) string {
	return t.colorize(ansiCyan, text)
}

// Yellow returns yellow text
func (t *termStyle) Yellow(text string) string {
	return t.colorize(ansiYellow, text)
}

// Green returns green text
func (t *termStyle) Green(text string) string {
	return t.colorize(ansiGreen, text)
}

// Red returns red text
func (t *termStyle) Red(text string) string {
	return t.colorize(ansiRed, text)
}

// Info prints informational/explanatory text (dimmed)
func (t *termStyle) Info(lines ...string) {
	for _, line := range lines {
		fmt.Println(t.Dim(line))
	}
}

// Print prints normal text
func (t *termStyle) Print(text string) {
	fmt.Print(text)
}

// Println prints normal text with newline
func (t *termStyle) Println(text string) {
	fmt.Println(text)
}

// Printf prints formatted text
func (t *termStyle) Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// List prints a numbered list with dimmed numbers
func (t *termStyle) List(items []string) {
	for i, item := range items {
		fmt.Printf("  %s %s\n", t.Dim(fmt.Sprintf("%d.", i+1)), item)
	}
}

// Bullet prints a bullet point
func (t *termStyle) Bullet(text string) {
	fmt.Printf("  • %s\n", text)
}

// KeyValue prints a key-value pair for summaries
func (t *termStyle) KeyValue(key, value string) {
	fmt.Printf("  %s  %s\n", t.Bold(fmt.Sprintf("%-18s", key+":")), value)
}

// Code prints a code block or command (indented and cyan)
func (t *termStyle) Code(lines ...string) {
	for _, line := range lines {
		fmt.Printf("     %s\n", t.Cyan(line))
	}
}

// Blank prints a blank line
func (t *termStyle) Blank() {
	fmt.Println()
}
