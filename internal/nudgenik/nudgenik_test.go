package nudgenik

import (
	"context"
	"errors"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestAskForCaptureNoResponse(t *testing.T) {
	cfg := &config.Config{
		Nudgenik: &config.NudgenikConfig{Target: "claude"},
	}

	_, err := AskForCapture(context.Background(), cfg, "❯\n")
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("expected ErrNoResponse, got %v", err)
	}
}

func TestAskForCaptureAgentMissing(t *testing.T) {
	if _, found, err := detect.FindDetectedTool(context.Background(), "claude"); err == nil && found {
		t.Skip("claude detected; skipping missing agent test")
	}

	cfg := &config.Config{
		Nudgenik: &config.NudgenikConfig{Target: "claude"},
	}

	_, err := AskForCapture(context.Background(), cfg, "hello\n❯\n")
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestAskForCaptureDisabled(t *testing.T) {
	cfg := &config.Config{}

	_, err := AskForCapture(context.Background(), cfg, "hello\n❯\n")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}
