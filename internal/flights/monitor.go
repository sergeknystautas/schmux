package flights

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

const monitorTick = 30 * time.Second

type monitorSubsystem struct {
	mu         sync.Mutex
	start      sync.Once
	workers    sync.WaitGroup
	done       chan struct{}
	kick       chan struct{}
	flights    map[string]*monitorFlight
	http       *http.Client
	configPath string
}

type monitorFlight struct {
	cancel         context.CancelFunc
	planner, pilot string
	wake           chan struct{}
}

func newMonitorSubsystem() *monitorSubsystem {
	return &monitorSubsystem{kick: make(chan struct{}, 1), done: make(chan struct{}), flights: map[string]*monitorFlight{}, http: &http.Client{Timeout: monitorHTTPTimeout}}
}

func (m *Manager) monitorConfigPath() string {
	if m.monitor.configPath != "" {
		return m.monitor.configPath
	}
	return filepath.Join(schmuxdir.Get(), "flight-monitor.yaml")
}

// StartMonitoring may run before Bind supplies chat sessions; reconciliation
// stays idle until then, and Bind wakes it.
func (m *Manager) StartMonitoring(ctx context.Context) {
	m.monitor.start.Do(func() { go func() { defer close(m.monitor.done); m.runMonitor(ctx) }() })
}

func (m *Manager) runMonitor(ctx context.Context) {
	if err := ensureMonitorConfig(m.monitorConfigPath()); err != nil {
		m.logger.Warn("monitor: ensure config failed", "err", err)
	}
	defer func() {
		m.monitor.mu.Lock()
		for id, worker := range m.monitor.flights {
			worker.cancel()
			delete(m.monitor.flights, id)
		}
		m.monitor.mu.Unlock()
		m.monitor.workers.Wait()
	}()
	ticker := time.NewTicker(monitorTick)
	defer ticker.Stop()
	for {
		if err := m.reconcileMonitor(ctx); err != nil {
			m.logger.Warn("monitor: reconcile failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.monitor.kick:
		case <-ticker.C:
		}
	}
}

func (m *Manager) kickMonitor() {
	if m.monitor == nil {
		return
	}
	select {
	case m.monitor.kick <- struct{}{}:
	default:
	}
}

func (m *Manager) reconcileMonitor(ctx context.Context) error {
	// Bind sets Chats under m.mu after monitoring starts. Workers are created
	// only after this locked read sees it, so their later reads are ordered.
	m.mu.Lock()
	chats := m.deps.Chats
	m.mu.Unlock()
	if chats == nil {
		return nil
	}
	active := map[string]bool{}
	for _, f := range m.List() {
		planner, pilot := f.sessionFor(RolePlanner), f.sessionFor(RolePilot)
		if f.WorkflowState != StatePlanning || f.Workspace == nil || f.CleanupState != CleanupIdle || planner == nil || pilot == nil || planner.SessionID == "" || pilot.SessionID == "" || planner.Outcome != StatusSucceeded || pilot.Outcome != StatusSucceeded {
			continue
		}
		active[f.ID] = true
		m.ensureMonitorFlight(ctx, f, pilot.SessionID, planner.SessionID)
	}
	m.monitor.mu.Lock()
	defer m.monitor.mu.Unlock()
	for id, worker := range m.monitor.flights {
		if !active[id] {
			worker.cancel()
			delete(m.monitor.flights, id)
		}
	}
	return nil
}

func (m *Manager) ensureMonitorFlight(ctx context.Context, f Flight, pilotID, plannerID string) {
	m.monitor.mu.Lock()
	defer m.monitor.mu.Unlock()
	if old := m.monitor.flights[f.ID]; old != nil {
		if old.pilot == pilotID && old.planner == plannerID {
			select {
			case old.wake <- struct{}{}:
			default:
			}
			return
		}
		old.cancel()
	}
	flightCtx, cancel := context.WithCancel(ctx)
	worker := &monitorFlight{cancel: cancel, planner: plannerID, pilot: pilotID, wake: make(chan struct{}, 1)}
	m.monitor.flights[f.ID] = worker
	m.monitor.workers.Add(1)
	go func() {
		defer m.monitor.workers.Done()
		defer func() {
			cancel()
			m.monitor.mu.Lock()
			if m.monitor.flights[f.ID] == worker {
				delete(m.monitor.flights, f.ID)
			}
			m.monitor.mu.Unlock()
		}()
		var ingestion sync.WaitGroup
		for _, sessionID := range []string{plannerID, pilotID} {
			ingestion.Add(1)
			go func() { defer ingestion.Done(); m.runMonitorFlight(flightCtx, f.ID, sessionID) }()
		}
		defer func() { cancel(); ingestion.Wait() }()
		for {
			m.processMonitorWork(flightCtx, f.ID, pilotID, plannerID)
			select {
			case <-flightCtx.Done():
				return
			case <-worker.wake:
			}
		}
	}()
}

func (m *Manager) runMonitorFlight(ctx context.Context, flightID, sessionID string) {
	for ctx.Err() == nil {
		rt, err := m.deps.Chats.GetChatRuntime(sessionID)
		if err == nil {
			err = m.consumeFlightOnce(ctx, flightID, sessionID, rt, lastRetainedEndSeqFor(flightID, sessionID, m))
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if recordErr := m.recordMonitorSessionError(flightID, sessionID, err); recordErr != nil {
				m.logger.Warn("monitor: retain session error failed", "err", recordErr)
			}
		}
		// A closed or unavailable runtime is retried at the reconciliation interval.
		select {
		case <-ctx.Done():
			return
		case <-time.After(monitorTick):
		}
	}
}

func (m *Manager) consumeFlightOnce(ctx context.Context, flightID, sessionID string, rt *chat.Runtime, after uint64) error {
	sub, err := rt.Subscribe(after)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer rt.Unsubscribe(sub.Live)
	if err := m.clearMonitorSessionError(flightID, sessionID); err != nil {
		m.logger.Warn("monitor: clear session error failed", "err", err)
	}
	collector := newTurnCollector(rt.Protocol(), sessionID)
	consume := func(rec chat.Record) error {
		turn, err := collector.consume(rec)
		if err != nil {
			return err
		}
		if turn != nil {
			_, err = m.recordPlannerTurn(flightID, *turn)
		}
		return err
	}
	for _, rec := range sub.History {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := consume(rec); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case rec, ok := <-sub.Live:
			if !ok {
				return nil
			}
			if err := consume(rec); err != nil {
				return err
			}
		}
	}
}

// Ingestion only persists turns. This worker drains retained work independently,
// including retries and turns whose subscription cursor already advanced.
func (m *Manager) processMonitorWork(ctx context.Context, flightID, pilotID, plannerID string) {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return
	}
	for _, t := range f.Monitor.Turns {
		if ctx.Err() != nil {
			return
		}
		if t.Turn.SessionID == pilotID {
			if t.Status == MonitoredTurnPending {
				m.processPilotTurn(ctx, flightID, t)
			}
			continue
		}
		if t.Turn.SessionID != plannerID {
			continue
		}
		if t.Status == MonitoredTurnPending {
			if t.Artifacts == nil && t.Turn.Outcome == "completed" {
				m.recordArtifactReport(ctx, flightID, t)
			}
			m.classifyTurn(ctx, flightID, t)
		}
		// Reconstruct a missing notification after a crash between the two writes.
		current, exists := m.findMonitoredTurn(flightID, t.ID)
		if !exists || current.Status == MonitoredTurnFailed || current.Status == MonitoredTurnPending {
			continue
		}
		notification := m.notificationForTurn(f, current, pilotID)
		if notification != nil {
			if _, err := m.recordNotificationIfAbsent(flightID, *notification); err != nil {
				m.logger.Warn("monitor: notification failed", "err", err)
			}
		}
	}
	f, ok = m.Get(flightID)
	if !ok || f.Monitor == nil {
		return
	}
	for _, msg := range f.Monitor.Messages {
		if ctx.Err() != nil {
			return
		}
		if msg.Status == MessageStatusPending {
			if _, err := m.deliverFlightMessage(ctx, flightID, msg.ID); err != nil {
				m.logger.Warn("monitor: delivery failed", "err", err)
			}
		} else if msg.Status == MessageStatusQueued {
			m.observeQueuedMessage(flightID, msg)
		}
	}
}

func (m *Manager) classifyTurn(ctx context.Context, flightID string, retained MonitoredTurn) {
	turn := retained.Turn
	attempt := MonitorAttempt{CreatedAt: m.deps.Now()}
	if turn.Outcome == "completed" && strings.TrimSpace(turn.Text) != "" {
		cfg, err := loadMonitorConfig(m.monitorConfigPath())
		if err != nil {
			attempt.Error = err.Error()
		} else {
			attempt.Config = &cfg
			if role, ok := cfg.Roles[string(RolePlanner)]; ok && len(role.Classifiers) > 0 {
				key, err := config.GetTypesafeAPIKey()
				if err != nil {
					attempt.Error = fmt.Sprintf("flight-monitor: read TypeSafe credentials: %v", err)
				} else {
					classification, err := classifyResponse(ctx, m.monitor.http, key, cfg, RolePlanner, turn.Text)
					if err != nil {
						attempt.Error = err.Error()
					} else {
						attempt.Classification = &classification
					}
				}
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	if err := m.recordMonitorAttempt(flightID, retained.ID, attempt); err != nil {
		m.logger.Warn("monitor: retain attempt failed", "err", err)
	}
}

func (m *Manager) notificationForTurn(f Flight, t MonitoredTurn, pilotID string) *FlightMessage {
	turn := t.Turn
	lines := []string{
		fmt.Sprintf("Flight %s; planner %s; records %d–%d; outcome %s", f.ID, turn.SessionID, turn.StartSeq, turn.EndSeq, turn.Outcome),
		"Conversation: " + chat.ConversationPath(schmuxdir.ChatSessionDir(f.Workspace.WorkspaceID, turn.SessionID)),
	}
	violation := t.Artifacts != nil && t.Artifacts.Error != ""
	if t.Artifacts == nil {
		lines = append(lines, "Spec/plan report: none in this response")
	} else {
		lines = append(lines, "Reported spec: "+t.Artifacts.SpecPath, "Reported plan: "+t.Artifacts.PlanPath)
		if violation {
			lines = append(lines, "Spec/plan check FAILED: "+t.Artifacts.Error)
		} else {
			lines = append(lines, "Spec/plan check: passed")
		}
	}
	if turn.Error != "" {
		lines = append(lines, "Harness error: "+turn.Error)
	}
	if turn.Outcome == "completed" {
		// A placement violation always reaches the pilot; otherwise the
		// latest classification decides.
		classified := len(t.Attempts) > 0 && t.Attempts[len(t.Attempts)-1].Config != nil && t.Attempts[len(t.Attempts)-1].Classification != nil
		if !classified && !violation {
			return nil
		}
		if classified {
			attempt := t.Attempts[len(t.Attempts)-1]
			defs := attempt.Config.Roles[string(RolePlanner)].Classifiers
			if !violation && !shouldNotify(defs, attempt.Classification.Answers) {
				return nil
			}
			for _, def := range defs {
				lines = append(lines, "Question "+def.ID+": "+def.Question)
				for _, choice := range def.Choices {
					lines = append(lines, "  "+choice.ID+": "+choice.Description)
				}
			}
			for _, answer := range attempt.Classification.Answers {
				lines = append(lines, fmt.Sprintf("%s: %s (confidence %.2f)", answer.ID, answer.Choice, *answer.Confidence))
			}
		}
	}
	lines = append(lines, "Check any claimed artifact in the workspace before relying on it.", "Full response:", turn.Text)
	return &FlightMessage{Kind: "notification", SourceTurnID: t.ID, RecipientSessionID: pilotID, Text: strings.Join(lines, "\n"), Status: MessageStatusPending}
}

func (m *Manager) recordNotificationIfAbsent(flightID string, msg FlightMessage) (FlightMessage, error) {
	f, ok := m.Get(flightID)
	if ok && f.Monitor != nil {
		for _, existing := range f.Monitor.Messages {
			if existing.Kind == "notification" && existing.SourceTurnID == msg.SourceTurnID {
				return existing, nil
			}
		}
	}
	return m.recordFlightMessage(flightID, msg)
}

func lastRetainedEndSeqFor(flightID, plannerID string, m *Manager) uint64 {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return 0
	}
	var last uint64
	for _, t := range f.Monitor.Turns {
		if t.Turn.SessionID == plannerID && t.Turn.EndSeq > last {
			last = t.Turn.EndSeq
		}
	}
	return last
}
