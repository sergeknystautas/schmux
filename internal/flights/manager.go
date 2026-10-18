package flights

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/spawnlaunch"
	"github.com/sergeknystautas/schmux/internal/state"
)

var (
	// ErrNotFound means no flight has the requested ID.
	ErrNotFound = errors.New("flight not found")
	// ErrInvalid means the request failed validation.
	ErrInvalid = errors.New("invalid flight request")
	// ErrConflict means the request is valid but not allowed in the flight's
	// current state, or a confirmation did not match.
	ErrConflict = errors.New("flight state conflict")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func conflict(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, args...))
}

const maxShortNameRunes = 80

// Repos looks up configured repositories.
type Repos interface {
	FindRepoByURL(url string) (config.Repo, bool)
}

// Targets validates spawn targets.
type Targets interface {
	// CheckChat returns an error unless chat sessions are enabled and every
	// named target has a chat mode.
	CheckChat(targetNames []string) error
}

// WorkspaceAllocator creates or prepares the workspace a flight takes off in.
type WorkspaceAllocator interface {
	GetOrCreateWithLabel(ctx context.Context, repoURL, branch, label string) (*state.Workspace, error)
}

// LocalSpawner spawns one local session through the shared spawn path.
type LocalSpawner interface {
	LocalTarget(ctx context.Context, req spawnlaunch.LocalTargetRequest) (spawnlaunch.LocalTargetResult, error)
}

// CommitSettings supplies the session fence policy flight sessions follow.
type CommitSettings interface {
	// FenceMode is the configured fence mode.
	FenceMode() string
	// FenceInstalled reports whether the fence dependency is installed.
	FenceInstalled() bool
}

// Dependencies are the narrow collaborators the manager needs. Now, NewID, and
// Notify are optional.
type Dependencies struct {
	Repos      Repos
	Targets    Targets
	Workspaces WorkspaceAllocator
	Spawner    LocalSpawner
	Settings   CommitSettings
	Cleaner    DestructiveCleaner
	Now        func() time.Time
	NewID      func() string
	// Notify is called after every persisted change, outside the manager's
	// locks, so the dashboard can broadcast a fresh snapshot.
	Notify func()
}

// Manager is the sole writer of the flights snapshot.
type Manager struct {
	deps   Dependencies
	logger *log.Logger

	// mu guards path, loaded, and flights, and is held across persistence so
	// writes reach disk in the order they were applied.
	mu      sync.Mutex
	path    string
	loaded  bool
	flights []Flight

	// opLocks serialize whole operations (including slow spawns) per flight,
	// so mu is never held across external calls.
	opLocksMu sync.Mutex
	opLocks   map[string]*sync.Mutex
}

// New builds a manager. Call Load before using it.
func New(deps Dependencies, logger *log.Logger) *Manager {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = randomID
	}
	return &Manager{deps: deps, logger: logger, opLocks: map[string]*sync.Mutex{}}
}

func randomID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("flights: crypto/rand: %v", err))
	}
	return "fl-" + hex.EncodeToString(b)
}

// Binding supplies the collaborators that only exist once the dashboard server
// is built: it owns the shared spawn and disposal services and the broadcast.
type Binding struct {
	Targets  Targets
	Spawner  LocalSpawner
	Cleaner  DestructiveCleaner
	Settings CommitSettings
	Notify   func()
}

// Bind installs late-bound collaborators. Call it before the manager serves any
// request; the daemon constructs the manager first so the server can take it.
func (m *Manager) Bind(b Binding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deps.Targets = b.Targets
	m.deps.Spawner = b.Spawner
	m.deps.Cleaner = b.Cleaner
	m.deps.Settings = b.Settings
	m.deps.Notify = b.Notify
}

// Load reads and validates the snapshot at path. A missing file is an empty
// snapshot. An invalid file is an error and is left untouched. Loading never
// starts a transition or dispose attempt.
func (m *Manager) Load(path string) error {
	snap, err := loadSnapshot(path)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.path = path
	m.flights = snap.Flights
	m.loaded = true
	return nil
}

// List returns every flight: backlog flights in order, then started flights by
// creation time.
func (m *Manager) List() []Flight {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Flight, len(m.flights))
	for i, f := range m.flights {
		out[i] = f.clone()
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		aBacklog, bBacklog := a.WorkflowState == StateBacklog, b.WorkflowState == StateBacklog
		switch {
		case aBacklog != bBacklog:
			return aBacklog
		case aBacklog:
			return a.Order < b.Order
		case !a.CreatedAt.Equal(b.CreatedAt):
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	return out
}

// Get returns a copy of one flight.
func (m *Manager) Get(id string) (Flight, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.flights {
		if f.ID == id {
			return f.clone(), true
		}
	}
	return Flight{}, false
}

// CreateRequest is the input for a new backlog flight.
type CreateRequest struct {
	Repo          string
	Prompt        string
	ShortName     string
	PlannerModel  string
	ExecutorModel string
}

// UpdateRequest edits a flight; nil fields are left alone.
type UpdateRequest struct {
	WorkflowState *WorkflowState
	Repo          *string
	Prompt        *string
	ShortName     *string
	PlannerModel  *string
	ExecutorModel *string
}

// Create adds a flight at the end of the backlog.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (Flight, error) {
	req.ShortName = strings.TrimSpace(req.ShortName)
	if err := m.validateFields(req.Repo, req.Prompt, req.ShortName, req.PlannerModel, req.ExecutorModel); err != nil {
		return Flight{}, err
	}
	var created Flight
	err := m.edit(func(flights *[]Flight) error {
		next := 0
		taken := map[string]bool{}
		for _, f := range *flights {
			taken[f.ID] = true
			if f.WorkflowState == StateBacklog && f.Order >= next {
				next = f.Order + 1
			}
		}
		id := m.deps.NewID()
		for taken[id] {
			id = m.deps.NewID()
		}
		now := m.deps.Now()
		created = Flight{
			ID: id, Repo: req.Repo, Prompt: req.Prompt, ShortName: req.ShortName,
			PlannerModel: req.PlannerModel, ExecutorModel: req.ExecutorModel,
			WorkflowState: StateBacklog, CleanupState: CleanupIdle, Order: next,
			CreatedAt: now, UpdatedAt: now,
		}
		*flights = append(*flights, created)
		return nil
	})
	if err != nil {
		return Flight{}, err
	}
	return created.clone(), nil
}

// Update edits the recorded stage without launching workflow operations.
// Field edits leave existing workspace and session assignments intact.
func (m *Manager) Update(ctx context.Context, id string, req UpdateRequest) (Flight, error) {
	unlock := m.lockFlight(id)
	defer unlock()

	err := m.editFlight(id, func(f *Flight) error {
		fieldsChanged := req.Repo != nil || req.Prompt != nil || req.ShortName != nil || req.PlannerModel != nil || req.ExecutorModel != nil
		next := *f
		if req.WorkflowState != nil {
			if !req.WorkflowState.valid() {
				return invalid("unknown workflow_state %q", *req.WorkflowState)
			}
			next.WorkflowState = *req.WorkflowState
			if next.WorkflowState == StateBacklog && f.WorkflowState != StateBacklog {
				next.Order = 0
				for _, other := range m.flights {
					if other.WorkflowState == StateBacklog && other.Order >= next.Order {
						next.Order = other.Order + 1
					}
				}
			}
		}
		if req.Repo != nil {
			next.Repo = *req.Repo
		}
		if req.Prompt != nil {
			next.Prompt = *req.Prompt
		}
		if req.ShortName != nil {
			next.ShortName = strings.TrimSpace(*req.ShortName)
		}
		if req.PlannerModel != nil {
			next.PlannerModel = *req.PlannerModel
		}
		if req.ExecutorModel != nil {
			next.ExecutorModel = *req.ExecutorModel
		}
		if fieldsChanged {
			if err := m.validateFields(next.Repo, next.Prompt, next.ShortName, next.PlannerModel, next.ExecutorModel); err != nil {
				return err
			}
		}
		*f = next
		return nil
	})
	if err != nil {
		return Flight{}, err
	}
	updated, _ := m.Get(id)
	return updated, nil
}

// ReorderBacklog sets the backlog order. ids must name every backlog flight
// exactly once.
func (m *Manager) ReorderBacklog(ctx context.Context, ids []string) error {
	return m.edit(func(flights *[]Flight) error {
		byID := map[string]int{}
		backlog := 0
		for i, f := range *flights {
			byID[f.ID] = i
			if f.WorkflowState == StateBacklog {
				backlog++
			}
		}
		seen := map[string]bool{}
		for _, id := range ids {
			i, ok := byID[id]
			if !ok {
				return invalid("unknown flight %q", id)
			}
			if (*flights)[i].WorkflowState != StateBacklog {
				return conflict("flight %s is %s; only backlog flights can be reordered", id, (*flights)[i].WorkflowState)
			}
			if seen[id] {
				return invalid("flight %q listed more than once", id)
			}
			seen[id] = true
		}
		if len(ids) != backlog {
			return invalid("order lists %d flights but the backlog has %d", len(ids), backlog)
		}
		now := m.deps.Now()
		for order, id := range ids {
			f := &(*flights)[byID[id]]
			f.Order = order
			f.UpdatedAt = now
		}
		return nil
	})
}

func (m *Manager) validateFields(repo, prompt, shortName, planner, executor string) error {
	if repo == "" {
		return invalid("repository is required")
	}
	if _, ok := m.deps.Repos.FindRepoByURL(repo); !ok {
		return invalid("repository %q is not configured", repo)
	}
	if strings.TrimSpace(prompt) == "" {
		return invalid("prompt is required")
	}
	if utf8.RuneCountInString(shortName) > maxShortNameRunes {
		return invalid("short_name is longer than %d characters", maxShortNameRunes)
	}
	if strings.IndexFunc(shortName, unicode.IsControl) >= 0 {
		return invalid("short_name must not contain control characters")
	}
	for field, target := range map[string]string{"planner_model": planner, "executor_model": executor} {
		if target == "" {
			return invalid("%s is required", field)
		}
		if err := m.deps.Targets.CheckChat([]string{target}); err != nil {
			return invalid("%s: %v", field, err)
		}
	}
	return nil
}

// lockFlight serializes whole operations on one flight and returns the unlock.
func (m *Manager) lockFlight(id string) func() {
	m.opLocksMu.Lock()
	l, ok := m.opLocks[id]
	if !ok {
		l = &sync.Mutex{}
		m.opLocks[id] = l
	}
	m.opLocksMu.Unlock()
	l.Lock()
	return l.Unlock
}

// edit applies fn to a copy of the flight list, persists the result, and only
// then makes it visible. If fn or persistence fails, nothing changes. Notify
// runs after the lock is released.
func (m *Manager) edit(fn func(flights *[]Flight) error) error {
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return errors.New("flights: manager not loaded")
	}
	work := make([]Flight, len(m.flights))
	for i, f := range m.flights {
		work[i] = f.clone()
	}
	if err := fn(&work); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := writeSnapshot(m.path, snapshot{Flights: work}); err != nil {
		m.mu.Unlock()
		return err
	}
	m.flights = work
	notify := m.deps.Notify
	m.mu.Unlock()

	if notify != nil {
		notify()
	}
	return nil
}

// editFlight is edit for a change to one flight; fn mutates it in place and
// UpdatedAt is advanced.
func (m *Manager) editFlight(id string, fn func(f *Flight) error) error {
	return m.edit(func(flights *[]Flight) error {
		for i := range *flights {
			f := &(*flights)[i]
			if f.ID != id {
				continue
			}
			if err := fn(f); err != nil {
				return err
			}
			f.UpdatedAt = m.deps.Now()
			return nil
		}
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	})
}
