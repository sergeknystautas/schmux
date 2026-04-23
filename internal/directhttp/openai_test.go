package directhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallOpenAI_HappyPath(t *testing.T) {
	var gotBody openaiRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"branch\":\"feat/x\"}"}}]}`)
	}))
	defer server.Close()

	raw, err := callOpenAI(context.Background(), openaiCallParams{
		Endpoint: server.URL,
		Request:  buildOpenAIRequest("llama3.2:latest", "pick a branch", "be terse", 0),
	})
	if err != nil {
		t.Fatalf("callOpenAI: %v", err)
	}
	if raw != `{"branch":"feat/x"}` {
		t.Errorf("raw: %q", raw)
	}
	if gotBody.Model != "llama3.2:latest" {
		t.Errorf("model: %q", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" || gotBody.Messages[1].Role != "user" {
		t.Errorf("messages: %+v", gotBody.Messages)
	}
}

func TestCallOpenAI_Non2xxReturnsErrHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
	}))
	defer server.Close()
	_, err := callOpenAI(context.Background(), openaiCallParams{Endpoint: server.URL, Request: buildOpenAIRequest("m", "p", "s", 0)})
	if !errors.Is(err, ErrHTTP) {
		t.Fatalf("got %v", err)
	}
}
