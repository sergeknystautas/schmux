package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

const refreshInterval = time.Minute

// Collector reads coding-plan quotas using the same provider keys used to run
// models. It does not depend on sessions, model enablement, or panel visibility.
type Collector struct {
	manager *Manager
	secrets func(string) (map[string]string, error)
	client  *http.Client
	logger  *log.Logger
}

func NewCollector(manager *Manager, secrets func(string) (map[string]string, error), logger *log.Logger) *Collector {
	return &Collector{
		manager: manager, secrets: secrets, logger: logger,
		client: &http.Client{
			Timeout: 15 * time.Second,
			// Never forward provider credentials to a redirect destination.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Run fetches immediately, then once per minute until the daemon stops.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		c.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Collector) refresh(ctx context.Context) {
	for _, provider := range []string{"moonshot", "zai", "minimax"} {
		if ctx.Err() != nil {
			return
		}
		secrets, err := c.secrets(provider)
		if err != nil {
			c.warn(provider, "cannot load provider credentials")
			continue
		}
		key := strings.TrimSpace(secrets["ANTHROPIC_AUTH_TOKEN"])
		if key == "" {
			continue
		}
		report, err := c.fetch(ctx, provider, key)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.warn(provider, err.Error())
			continue // A failed fetch must not erase the last successful snapshot.
		}
		c.manager.Observe(provider, report)
	}
}

func (c *Collector) warn(provider, reason string) {
	if c.logger != nil {
		c.logger.Warn("plan usage fetch failed", "provider", provider, "reason", reason)
	}
}

func (c *Collector) fetch(ctx context.Context, provider, key string) (contracts.UsageProviderInfo, error) {
	var endpoint string
	switch provider {
	case "moonshot":
		endpoint = "https://api.kimi.com/coding/v1/usages"
	case "zai":
		endpoint = "https://api.z.ai/api/monitor/usage/quota/limit"
	case "minimax":
		endpoint = "https://api.minimax.io/v1/token_plan/remains"
	default:
		return contracts.UsageProviderInfo{}, fmt.Errorf("unsupported usage provider")
	}
	body, status, err := c.get(ctx, endpoint, key)
	// Older MiniMax coding plans use a different endpoint on the same host.
	if provider == "minimax" && status == http.StatusNotFound {
		body, _, err = c.get(ctx, "https://api.minimax.io/v1/api/openplatform/coding_plan/remains", key)
	}
	if err != nil {
		return contracts.UsageProviderInfo{}, err
	}
	var report contracts.UsageProviderInfo
	switch provider {
	case "moonshot":
		report, err = parseKimiQuota(body)
	case "zai":
		report, err = parseZaiQuota(body)
	case "minimax":
		report, err = parseMiniMaxQuota(body)
	}
	return report, err
}

func (c *Collector) get(ctx context.Context, endpoint, key string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid quota endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "schmux")
	if req.URL.Host == "api.minimax.io" {
		req.Header.Set("MM-API-Source", "schmux")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		// Transport errors and provider bodies can contain credentials. Keep
		// diagnostics structural; never log those strings.
		return nil, 0, fmt.Errorf("quota request failed (network, timeout, or cancellation)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("quota HTTP status %d", resp.StatusCode)
	}
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return nil, resp.StatusCode, fmt.Errorf("cannot read quota response")
	}
	return body, resp.StatusCode, nil
}

func quotaReport(windows []contracts.UsageWindow) (contracts.UsageProviderInfo, error) {
	if len(windows) == 0 {
		return contracts.UsageProviderInfo{}, fmt.Errorf("no supported quota windows in response")
	}
	return contracts.UsageProviderInfo{Windows: windows}, nil
}

func percentage(value float64) *float64 {
	v := max(0, min(100, value))
	return &v
}

func numeric(n *json.Number) (float64, bool) {
	if n == nil {
		return 0, false
	}
	v, err := n.Float64()
	return v, err == nil
}

func unixReset(value string) int64 {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return t.Unix()
}
