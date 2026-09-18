package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

type managerState struct {
	Providers map[string]contracts.UsageProviderInfo `json:"providers"`
}

// Manager persists the latest reported plan quota per provider. It receives
// live events; it never scans conversations or computes consumption totals.
type Manager struct {
	mu       sync.Mutex
	dataPath string
	logger   *log.Logger
	st       managerState
}

func NewManager(dataPath string, logger *log.Logger) *Manager {
	return &Manager{dataPath: dataPath, logger: logger, st: managerState{Providers: map[string]contracts.UsageProviderInfo{}}}
}

// Observe replaces a provider's snapshot with a parsed quota report.
func (m *Manager) Observe(provider string, reported contracts.UsageProviderInfo) {
	if provider == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reported.Provider = provider
	reported.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	reported = cloneReport(reported)
	previous, existed := m.st.Providers[provider]
	m.st.Providers[provider] = reported
	if m.dataPath == "" {
		return
	}
	if err := m.saveLocked(); err != nil {
		if existed {
			m.st.Providers[provider] = previous
		} else {
			delete(m.st.Providers, provider)
		}
		if m.logger != nil {
			m.logger.Warn("failed to persist plan usage", "provider", provider, "err", err)
		}
	}
}

func cloneReport(p contracts.UsageProviderInfo) contracts.UsageProviderInfo {
	p.Windows = append([]contracts.UsageWindow{}, p.Windows...)
	for i := range p.Windows {
		if p.Windows[i].UsedPercent != nil {
			v := *p.Windows[i].UsedPercent
			p.Windows[i].UsedPercent = &v
		}
	}
	if p.Credits != nil {
		c := *p.Credits
		p.Credits = &c
	}
	if p.IsUsingOverage != nil {
		v := *p.IsUsingOverage
		p.IsUsingOverage = &v
	}
	return p
}

func (m *Manager) Snapshot() []contracts.UsageProviderInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	providers := make([]contracts.UsageProviderInfo, 0, len(m.st.Providers))
	for _, p := range m.st.Providers {
		providers = append(providers, cloneReport(p))
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Provider < providers[j].Provider })
	return providers
}

func (m *Manager) Load() {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, err := os.ReadFile(m.dataPath)
	if err != nil {
		if !os.IsNotExist(err) && m.logger != nil {
			m.logger.Warn("failed to read plan usage", "err", err)
		}
		return
	}
	var st managerState
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&st); err != nil {
		if m.logger != nil {
			m.logger.Warn("failed to parse stored plan usage", "err", err)
		}
		return
	}
	if st.Providers != nil {
		m.st = st
	}
}

func (m *Manager) saveLocked() error {
	body, err := json.MarshalIndent(m.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.dataPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.dataPath)
}
