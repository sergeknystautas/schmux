package usage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/charmbracelet/log"
)

type quotaTransport func(*http.Request) (*http.Response, error)

func (f quotaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func quotaResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestCollectorPersistsAndKeepsLastSuccessfulSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	m := NewManager(path, nil)
	var logs bytes.Buffer
	keys := map[string]string{"moonshot": "test-kimi-key", "zai": "test-zai-key", "minimax": "test-minimax-key"}
	c := NewCollector(m, func(provider string) (map[string]string, error) {
		return map[string]string{"ANTHROPIC_AUTH_TOKEN": keys[provider]}, nil
	}, log.New(&logs))
	fail := false
	requests := 0
	c.client.Transport = quotaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		var provider, fixture, path string
		switch r.URL.Host {
		case "api.kimi.com":
			provider, fixture, path = "moonshot", kimiQuotaFixture, "/coding/v1/usages"
		case "api.z.ai":
			provider, fixture, path = "zai", zaiQuotaFixture, "/api/monitor/usage/quota/limit"
		case "api.minimax.io":
			provider, fixture, path = "minimax", minimaxQuotaFixture, "/v1/token_plan/remains"
		default:
			t.Fatalf("unexpected endpoint %s", r.URL)
		}
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer "+keys[provider] || r.Header.Get("User-Agent") != "schmux" {
			t.Fatalf("incorrect request for %s", provider)
		}
		if fail {
			return quotaResponse(401, keys[provider]), nil
		}
		return quotaResponse(200, fixture), nil
	})
	c.refresh(context.Background())
	before := m.Snapshot()
	if requests != 3 || len(before) != 3 {
		t.Fatalf("requests=%d snapshots=%+v", requests, before)
	}
	fail = true
	c.refresh(context.Background())
	after := m.Snapshot()
	for i, p := range after {
		if p.UpdatedAt != before[i].UpdatedAt {
			t.Errorf("failure replaced %s snapshot", p.Provider)
		}
	}
	reloaded := NewManager(path, nil)
	reloaded.Load()
	if got := reloaded.Snapshot(); len(got) != 3 {
		t.Fatalf("persisted snapshots=%+v", got)
	}
	for _, key := range keys {
		if strings.Contains(logs.String(), key) {
			t.Fatal("credential leaked to log")
		}
	}
	if !strings.Contains(logs.String(), "401") {
		t.Fatalf("missing structural failure diagnostic: %s", logs.String())
	}
}

func TestCollectorRefreshReloadsCredentialsAndIsolatesFailures(t *testing.T) {
	m := NewManager("", nil)
	configured := false
	c := NewCollector(m, func(provider string) (map[string]string, error) {
		if provider == "zai" {
			return nil, fmt.Errorf("credential load failed")
		}
		if provider == "moonshot" || !configured {
			return nil, nil
		}
		return map[string]string{"ANTHROPIC_AUTH_TOKEN": "key"}, nil
	}, nil)
	requests := 0
	c.client.Transport = quotaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return quotaResponse(200, minimaxQuotaFixture), nil
	})
	c.refresh(context.Background())
	if requests != 0 {
		t.Fatalf("unconfigured provider requested %d times", requests)
	}
	configured = true
	c.refresh(context.Background())
	if got := m.Snapshot(); requests != 1 || len(got) != 1 || got[0].Provider != "minimax" {
		t.Fatalf("requests=%d snapshot=%+v", requests, got)
	}
}

func TestCollectorMiniMaxLegacyFallbackAndRedirectRejection(t *testing.T) {
	c := NewCollector(NewManager("", nil), nil, nil)
	requests := 0
	c.client.Transport = quotaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Path == "/v1/token_plan/remains" {
			return quotaResponse(404, ""), nil
		}
		if r.URL.Host != "api.minimax.io" || r.URL.Path != "/v1/api/openplatform/coding_plan/remains" {
			t.Fatalf("unexpected fallback: %s", r.URL)
		}
		return quotaResponse(200, minimaxQuotaFixture), nil
	})
	if _, err := c.fetch(context.Background(), "minimax", "key"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
	requests = 0
	c.client.Transport = quotaTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		response := quotaResponse(302, "")
		response.Header.Set("Location", "https://other.example/quota")
		return response, nil
	})
	if _, err := c.fetch(context.Background(), "moonshot", "secret"); err == nil {
		t.Fatal("redirect accepted")
	}
	if requests != 1 {
		t.Fatalf("followed credential redirect: %d requests", requests)
	}
}

func TestCollectorRunsImmediatelyEveryMinuteAndStops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		c := NewCollector(NewManager("", nil), func(provider string) (map[string]string, error) {
			if provider == "moonshot" {
				calls.Add(1)
			}
			return nil, nil
		}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); c.Run(ctx) }()
		synctest.Wait()
		if got := calls.Load(); got != 1 {
			t.Fatalf("startup refresh count=%d", got)
		}
		// synctest advances a virtual clock: prove the authored one-minute cadence.
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("minute refresh count=%d", got)
		}
		cancel()
		<-done
		time.Sleep(time.Minute)
		if got := calls.Load(); got != 2 {
			t.Fatalf("refresh after shutdown: %d", got)
		}
	})
}

func TestCollectorCancelsInFlightRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := NewManager("", nil)
		c := NewCollector(m, func(string) (map[string]string, error) { return map[string]string{"ANTHROPIC_AUTH_TOKEN": "key"}, nil }, nil)
		started := make(chan struct{})
		c.client.Transport = quotaTransport(func(r *http.Request) (*http.Response, error) {
			close(started)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); c.Run(ctx) }()
		<-started
		cancel()
		<-done
		if got := m.Snapshot(); len(got) != 0 {
			t.Fatalf("cancelled request recorded: %+v", got)
		}
	})
}
