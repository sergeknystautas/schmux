package nudgenik

import (
	"context"
	"errors"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
)

func TestAskForCaptureNoResponse(t *testing.T) {
	cfg := &config.Config{}
	cfg.Nudgenik = &config.NudgenikConfig{Target: "claude"}

	_, err := AskForCapture(context.Background(), cfg, "❯\n")
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("expected ErrNoResponse, got %v", err)
	}
}

func TestAskForCaptureDisabled(t *testing.T) {
	cfg := &config.Config{}

	_, err := AskForCapture(context.Background(), cfg, "hello\n❯\n")
	if !errors.Is(err, oneshot.ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}
