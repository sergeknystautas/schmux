package workspace

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

// DedicatedRegistration declares what a named dedicated workspace needs.
// A dedicated workspace is an ordinary workspace carrying a name; schmux
// creates it, keeps it, and hands it back on request.
type DedicatedRegistration struct {
	RepoURL string // a configured repo URL
	Branch  string
	Label   string // "" means the name
}

// RegisterDedicated declares a dedicated workspace and what it needs.
// Registering the same name again replaces its registration.
//
// A workspace that carries the name but no longer matches the registration's
// repo loses the name: the caller's old workspace must not keep serving a
// name that now points at another repo.
//
// A changed registration takes the name's lock, so one that races an in-flight
// Ensure waits for it and checks the workspace the Ensure left behind. An
// unchanged one returns at once: callers register again on every config save.
func (m *Manager) RegisterDedicated(name string, reg DedicatedRegistration) {
	if current, found := m.dedicatedRegistration(name); found && current == reg {
		return
	}

	lock := m.dedicatedLock(name)
	lock.Lock()
	defer lock.Unlock()

	m.dedicatedMu.Lock()
	m.dedicated[name] = reg
	m.dedicatedMu.Unlock()

	if ws, found := state.FindDedicatedWorkspace(m.state.GetWorkspaces(), name); found && ws.Repo != reg.RepoURL {
		if err := m.setDedicated(ws.ID, ""); err != nil {
			m.logger.Warn("failed to clear the dedicated name after a repo change", "name", name, "err", err)
		}
	}
}

// UnregisterDedicated drops the registration and removes the name from the
// workspace that carries it. The workspace stays where it is, as an ordinary
// workspace; the user decides whether to dispose it.
//
// It takes the name's lock, so an unregister that races an in-flight Ensure
// waits for it: the workspace the Ensure creates never keeps a name that no
// longer has a registration.
func (m *Manager) UnregisterDedicated(name string) error {
	lock := m.dedicatedLock(name)
	lock.Lock()
	defer lock.Unlock()

	m.dedicatedMu.Lock()
	delete(m.dedicated, name)
	m.dedicatedMu.Unlock()

	ws, found := state.FindDedicatedWorkspace(m.state.GetWorkspaces(), name)
	if !found {
		return nil
	}
	return m.setDedicated(ws.ID, "")
}

// RegisteredDedicated lists the names carried by workspaces in state that
// start with prefix, sorted. Callers reconcile at daemon start by
// unregistering the names under their prefix that their config no longer wants.
func (m *Manager) RegisteredDedicated(prefix string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, w := range m.state.GetWorkspaces() {
		if w.Dedicated == "" || !strings.HasPrefix(w.Dedicated, prefix) {
			continue
		}
		if seen[w.Dedicated] {
			continue
		}
		seen[w.Dedicated] = true
		names = append(names, w.Dedicated)
	}
	sort.Strings(names)
	return names
}

// EnsureDedicated returns the dedicated workspace for name, ready to use.
// It creates the workspace when there is none, and replaces one that no
// longer matches its registration. Calls for the same name run one at a time.
func (m *Manager) EnsureDedicated(ctx context.Context, name string) (*state.Workspace, error) {
	lock := m.dedicatedLock(name)
	lock.Lock()
	defer lock.Unlock()

	reg, found := m.dedicatedRegistration(name)
	if !found {
		return nil, fmt.Errorf("dedicated workspace %q is not registered", name)
	}

	if ws, ok := state.FindDedicatedWorkspace(m.state.GetWorkspaces(), name); ok {
		if m.dedicatedUsable(ws, reg) {
			return &ws, nil
		}
		// Unusable or mismatched: the name leaves the workspace, which stays
		// an ordinary workspace with the user's work in it.
		if err := m.setDedicated(ws.ID, ""); err != nil {
			return nil, err
		}
	}

	if err := ValidateBranchName(reg.Branch); err != nil {
		return nil, fmt.Errorf("dedicated workspace %q: %w", name, err)
	}

	// The repo lock is held across creation and stamping, so no spawn sees
	// the new workspace before it carries its name.
	repoLock := m.repoLock(reg.RepoURL)
	repoLock.Lock()
	defer repoLock.Unlock()

	label := reg.Label
	if label == "" {
		label = name
	}
	ws, err := m.createFresh(ctx, reg.RepoURL, reg.Branch, label)
	if err != nil {
		return nil, err
	}
	if ws.CreatedAt.IsZero() {
		ws.CreatedAt = time.Now()
		if err := m.state.UpdateWorkspace(*ws); err != nil {
			return nil, fmt.Errorf("failed to update workspace: %w", err)
		}
	}
	if err := m.setDedicated(ws.ID, name); err != nil {
		return nil, err
	}
	current, _ := m.state.GetWorkspace(ws.ID)
	return &current, nil
}

// dedicatedUsable reports whether the workspace carrying a name still
// satisfies its registration.
func (m *Manager) dedicatedUsable(ws state.Workspace, reg DedicatedRegistration) bool {
	if ws.Repo != reg.RepoURL {
		return false
	}
	if ws.Status == state.WorkspaceStatusDisposing {
		return false
	}
	if _, err := os.Stat(ws.Path); err != nil {
		return false
	}
	return true
}

// dedicatedRegistration returns the registration for a name.
func (m *Manager) dedicatedRegistration(name string) (DedicatedRegistration, bool) {
	m.dedicatedMu.Lock()
	defer m.dedicatedMu.Unlock()
	reg, found := m.dedicated[name]
	return reg, found
}

// dedicatedLock returns the per-name lock, creating it on first use.
func (m *Manager) dedicatedLock(name string) *sync.Mutex {
	m.dedicatedMu.Lock()
	defer m.dedicatedMu.Unlock()
	lock, ok := m.dedicatedLocks[name]
	if !ok {
		lock = &sync.Mutex{}
		m.dedicatedLocks[name] = lock
	}
	return lock
}

// setDedicated stamps a name on a workspace (or clears it with an empty
// name) and saves the state.
func (m *Manager) setDedicated(workspaceID, name string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	w.Dedicated = name
	if err := m.state.UpdateWorkspace(w); err != nil {
		return fmt.Errorf("failed to update workspace: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}
	return nil
}
