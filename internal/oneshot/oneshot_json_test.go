package oneshot

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testResult struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestParseJSON_PlainObject(t *testing.T) {
	got, err := ParseJSON[testResult](`{"name":"foo","value":7}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "foo" || got.Value != 7 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_StripsCodeFence(t *testing.T) {
	raw := "```json\n{\"name\":\"bar\",\"value\":1}\n```"
	got, err := ParseJSON[testResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "bar" || got.Value != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_HandlesBannerBeforeAndAfter(t *testing.T) {
	raw := "blah blah {\"name\":\"baz\",\"value\":42} trailing"
	got, err := ParseJSON[testResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "baz" || got.Value != 42 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_CurlyQuotesRecoveredByNormalize(t *testing.T) {
	raw := "{\u201cname\u201d:\u201chello\u201d,\u201cvalue\u201d:3}"
	got, err := ParseJSON[testResult](raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "hello" || got.Value != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseJSON_EmptyInput(t *testing.T) {
	_, err := ParseJSON[testResult]("")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
}

func TestParseJSON_NoBraces(t *testing.T) {
	_, err := ParseJSON[testResult]("nothing json-like here")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
}

func TestParseJSON_MalformedJSONBeyondRecovery(t *testing.T) {
	_, err := ParseJSON[testResult](`{"name": "unterminated`)
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("want ErrInvalidResponse, got %v", err)
	}
	if !strings.Contains(err.Error(), "unexpected end") && !strings.Contains(err.Error(), "unexpected EOF") && !strings.Contains(err.Error(), "no JSON") {
		t.Logf("note: underlying error is %q", err.Error())
	}
}

func TestExecuteTarget_EmptyTargetReturnsErrDisabled(t *testing.T) {
	_, err := ExecuteTarget(context.TODO(), nil, "", "some prompt", "", 0, "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
}

func TestExecuteTargetJSON_EmptyTargetReturnsErrDisabled(t *testing.T) {
	_, raw, err := ExecuteTargetJSON[testResult](context.TODO(), nil, "", "some prompt", "", 0, "")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if raw != "" {
		t.Fatalf("raw should be empty on pre-parse error, got %q", raw)
	}
}
