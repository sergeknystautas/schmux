package oneshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/directhttp"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// fakeAttempt records calls and replies per target from the replies map.
func fakeAttempt(replies map[string]error) (attemptFunc[string], *[]string) {
	var calls []string
	return func(ctx context.Context, cfg *config.Config, targetName, prompt, schemaLabel string, timeout time.Duration, dir string) (string, error) {
		calls = append(calls, targetName)
		if err, ok := replies[targetName]; ok {
			return "", err
		}
		return "ok:" + targetName, nil
	}, &calls
}

func rateLimit() error {
	return &directhttp.RateLimitError{Status: 429, Body: `{"type":"error","error":{"type":"rate_limit_error"}}`}
}

func TestRunTargetChain_PrimaryRateLimitedFallsBack(t *testing.T) {
	attempt, calls := fakeAttempt(map[string]error{"A::api": rateLimit()})
	out, err := runTargetChain[string](context.Background(), nil,
		[]string{"A::api", "B::api"}, "p", schema.LabelBranchSuggest, time.Second, "", attempt)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out != "ok:B::api" {
		t.Errorf("out = %q, want ok:B::api", out)
	}
	if len(*calls) != 2 || (*calls)[0] != "A::api" || (*calls)[1] != "B::api" {
		t.Errorf("calls = %v, want [A::api B::api]", *calls)
	}
}

func TestRunTargetChain_AllRateLimitedReturnsLastError(t *testing.T) {
	attempt, calls := fakeAttempt(map[string]error{
		"A::api": rateLimit(),
		"B::api": rateLimit(),
	})
	_, err := runTargetChain[string](context.Background(), nil,
		[]string{"A::api", "B::api"}, "p", schema.LabelBranchSuggest, time.Second, "", attempt)
	var rle *directhttp.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want RateLimitError from last target", err)
	}
	if len(*calls) != 2 {
		t.Errorf("calls = %v, want both targets attempted", *calls)
	}
}

func TestRunTargetChain_NonRateLimitErrorStopsImmediately(t *testing.T) {
	attempt, calls := fakeAttempt(map[string]error{
		"A::api": fmt.Errorf("some transport 500"),
	})
	_, err := runTargetChain[string](context.Background(), nil,
		[]string{"A::api", "B::api"}, "p", schema.LabelBranchSuggest, time.Second, "", attempt)
	if err == nil || err.Error() != "some transport 500" {
		t.Fatalf("err = %v, want the non-429 error verbatim", err)
	}
	if len(*calls) != 1 {
		t.Errorf("calls = %v, want only the first target", *calls)
	}
}

func TestRunTargetChain_EmptyListReturnsErrDisabled(t *testing.T) {
	attempt, _ := fakeAttempt(nil)
	_, err := runTargetChain[string](context.Background(), nil,
		nil, "p", schema.LabelBranchSuggest, time.Second, "", attempt)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestRunTargetChain_CanceledContextStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempt, calls := fakeAttempt(nil)
	_, err := runTargetChain[string](ctx, nil,
		[]string{"A::api"}, "p", schema.LabelBranchSuggest, time.Second, "", attempt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none", *calls)
	}
}

func TestExecuteTarget_WrapperEqualsSingleElementChain(t *testing.T) {
	// Existing behavior must be preserved: empty target → ErrDisabled.
	_, err := ExecuteTarget[string](context.Background(), nil, "", "p", schema.LabelBranchSuggest, time.Second, "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	if _, err := ExecuteTarget[string](context.Background(), nil, "x", "p", "", time.Second, ""); !errors.Is(err, ErrNoSchemaLabel) {
		t.Fatalf("err = %v, want ErrNoSchemaLabel", err)
	}
}

func TestExecuteTargetAttempt_WritesOneLogRecordPerAttempt(t *testing.T) {
	dir := t.TempDir()
	schmuxdir.Set(dir)
	defer schmuxdir.Set("")

	_, err := executeTargetAttempt[string](context.Background(), nil,
		"no-such-target", "prompt", schema.LabelBranchSuggest, time.Second, "")
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v, want ErrTargetNotFound", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "logs", "oneshot.jsonl"))
	if err != nil {
		t.Fatalf("read oneshot log: %v", err)
	}
	var rec contracts.OneshotLogRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode record: %v (line=%s)", err, data)
	}
	if rec.Model != "no-such-target" {
		t.Errorf("Model = %q, want no-such-target", rec.Model)
	}
	if rec.Transport != "cli" {
		t.Errorf("Transport = %q, want cli", rec.Transport)
	}
	if rec.OK {
		t.Error("OK should be false for a failed attempt")
	}
	if rec.Type != schema.LabelBranchSuggest {
		t.Errorf("Type = %q, want %q", rec.Type, schema.LabelBranchSuggest)
	}
}
