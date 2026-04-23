package directhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ProbeOllama GETs {endpoint}/api/tags and returns model names.
func ProbeOllama(endpoint string, timeout time.Duration) ([]string, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint")
	}
	endpoint = strings.TrimRight(endpoint, "/")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama /api/tags: %d %s", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed ollamaTagsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out, nil
}

type ollamaRegistry struct {
	mu     sync.RWMutex
	models []string
}

func newOllamaRegistry() *ollamaRegistry { return &ollamaRegistry{} }

func (r *ollamaRegistry) Update(models []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(models))
	copy(out, models)
	r.models = out
}

func (r *ollamaRegistry) Snapshot() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.models))
	copy(out, r.models)
	return out
}

var ollamaReg = newOllamaRegistry()

func SetOllamaModels(models []string) { ollamaReg.Update(models) }

func GetOllamaModels() []string { return ollamaReg.Snapshot() }

// ollamaAutoDetected holds the URL the probe loop auto-detected when the
// user's config has no Ollama endpoint set. It is never persisted to disk;
// it is exposed to the UI via handleConfigGet so users can see what is
// being used without a silent config.json mutation.
var (
	ollamaAutoDetectedMu  sync.RWMutex
	ollamaAutoDetectedURL string
)

func SetOllamaAutoDetectedEndpoint(url string) {
	ollamaAutoDetectedMu.Lock()
	defer ollamaAutoDetectedMu.Unlock()
	ollamaAutoDetectedURL = url
}

func GetOllamaAutoDetectedEndpoint() string {
	ollamaAutoDetectedMu.RLock()
	defer ollamaAutoDetectedMu.RUnlock()
	return ollamaAutoDetectedURL
}

// LoopOllamaProbe invokes refresh once immediately, then every interval until stop is closed.
func LoopOllamaProbe(interval time.Duration, refresh func(), stop <-chan struct{}) {
	refresh()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			refresh()
		}
	}
}
