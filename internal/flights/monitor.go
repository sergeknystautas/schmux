package flights

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

const monitorTick = 30 * time.Second

type monitorSubsystem struct {
	mu      sync.Mutex
	start   sync.Once
	workers sync.WaitGroup
	done    chan struct{}
	kick    chan struct{}
	flights map[string]*monitorFlight
	http    *http.Client
	// definitions is the classifier YAML: schmux's own monitorDefinitionsYAML.
	// Tests substitute other definitions.
	definitions []byte
}

// monitorFlight is the worker for one flight. key names the pilot and the
// working sessions it follows; a change in either restarts the worker.
type monitorFlight struct {
	cancel context.CancelFunc
	key    string
	wake   chan struct{}
}

func newMonitorSubsystem() *monitorSubsystem {
	return &monitorSubsystem{kick: make(chan struct{}, 1), done: make(chan struct{}), flights: map[string]*monitorFlight{}, http: &http.Client{Timeout: monitorHTTPTimeout}, definitions: monitorDefinitionsYAML}
}

// StartMonitoring may run before Bind supplies chat sessions; reconciliation
// stays idle until then, and Bind wakes it.
func (m *Manager) StartMonitoring(ctx context.Context) {
	m.monitor.start.Do(func() { go func() { defer close(m.monitor.done); m.runMonitor(ctx) }() })
}

func (m *Manager) runMonitor(ctx context.Context) {
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
		pilot, working, ok := monitoredSessions(f)
		if !ok {
			continue
		}
		active[f.ID] = true
		m.ensureMonitorFlight(ctx, f, pilot, working)
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

// workingRoles are the roles the pilot supervises and may address, in the
// order the flight starts them.
var workingRoles = []SessionRole{RolePlanner, RoleExecutor, RoleReviewer, RoleCommit}

// startedSession returns the role's current session when its spawn succeeded.
func startedSession(f Flight, role SessionRole) (string, bool) {
	s := f.sessionFor(role)
	if s == nil || s.SessionID == "" || s.Outcome != StatusSucceeded {
		return "", false
	}
	return s.SessionID, true
}

// monitoredSessions returns the pilot and the working sessions the monitor
// follows for a flight, keyed by session ID: every started planner, executor,
// reviewer, and committer, whatever the phase. The pilot orchestrates all of
// them, so the monitor follows all of them; which turns notify the pilot is
// decided per turn (notifiesEveryTurn and the classifiers). ok is false for a
// flight that is not supervised: a phase outside planning through completing,
// no workspace, cleanup under way, no started pilot, or no started working
// session.
func monitoredSessions(f Flight) (pilot string, working map[string]SessionRole, ok bool) {
	if f.Workspace == nil || f.CleanupState != CleanupIdle || !supervisedPhase(f.WorkflowState) {
		return "", nil, false
	}
	pilot, ok = startedSession(f, RolePilot)
	if !ok {
		return "", nil, false
	}
	working = map[string]SessionRole{}
	for _, role := range workingRoles {
		if id, ok := startedSession(f, role); ok {
			working[id] = role
		}
	}
	if len(working) == 0 {
		return "", nil, false
	}
	return pilot, working, true
}

func (m *Manager) ensureMonitorFlight(ctx context.Context, f Flight, pilotID string, working map[string]SessionRole) {
	followed := slices.Sorted(maps.Keys(working))
	key := pilotID + "|" + strings.Join(followed, "|")
	m.monitor.mu.Lock()
	defer m.monitor.mu.Unlock()
	if old := m.monitor.flights[f.ID]; old != nil {
		if old.key == key {
			select {
			case old.wake <- struct{}{}:
			default:
			}
			return
		}
		old.cancel()
	}
	flightCtx, cancel := context.WithCancel(ctx)
	worker := &monitorFlight{cancel: cancel, key: key, wake: make(chan struct{}, 1)}
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
		for _, sessionID := range append([]string{pilotID}, followed...) {
			ingestion.Add(1)
			go func() { defer ingestion.Done(); m.runMonitorFlight(flightCtx, f.ID, sessionID) }()
		}
		defer func() { cancel(); ingestion.Wait() }()
		for {
			m.processMonitorWork(flightCtx, f.ID)
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
func (m *Manager) processMonitorWork(ctx context.Context, flightID string) {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return
	}
	pilotID, working, ok := monitoredSessions(f)
	if !ok {
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
		role, ok := working[t.Turn.SessionID]
		if !ok {
			continue
		}
		if t.Status == MonitoredTurnPending {
			if t.Turn.Outcome == "completed" {
				switch {
				case role == RolePlanner && t.Artifacts == nil:
					m.recordArtifactReport(ctx, flightID, t)
				case role == RoleExecutor && t.Execution == nil:
					m.recordExecutionCheck(ctx, flightID, t)
				case role == RoleReviewer && t.Review == nil:
					m.recordReviewCheck(ctx, flightID, t)
				}
			}
			m.classifyTurn(ctx, flightID, t, role)
		}
		// Reconstruct a missing notification after a crash between the two writes.
		current, exists := m.findMonitoredTurn(flightID, t.ID)
		if !exists || current.Status == MonitoredTurnPending {
			continue
		}
		// A failed classification holds a planning turn back, but a supervised
		// execution turn is too important to leave unwatched over a
		// configuration problem.
		if current.Status == MonitoredTurnFailed && !notifiesEveryTurn(f, role) {
			continue
		}
		notification := m.notificationForTurn(f, current, pilotID, role)
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

func (m *Manager) classifyTurn(ctx context.Context, flightID string, retained MonitoredTurn, role SessionRole) {
	turn := retained.Turn
	attempt := MonitorAttempt{CreatedAt: m.deps.Now()}
	if turn.Outcome == "completed" && strings.TrimSpace(turn.Text) != "" {
		cfg, err := parseMonitorConfig(m.monitor.definitions)
		if err != nil {
			attempt.Error = err.Error()
		} else {
			attempt.Config = &cfg
			if def, ok := cfg.Roles[string(role)]; ok && len(def.Classifiers) > 0 {
				key, err := config.GetTypesafeAPIKey()
				if err != nil {
					attempt.Error = fmt.Sprintf("flight-monitor: read TypeSafe credentials: %v", err)
				} else {
					classification, err := classifyResponse(ctx, m.monitor.http, key, cfg, role, turn.Text)
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

// notifiesEveryTurn reports whether the pilot hears about this turn whatever
// the classifiers say. While a flight executes, every executor turn qualifies:
// most executions end in one turn, and the pilot's comparison with the plan is
// the drift check. While a flight reviews, every reviewer turn qualifies: the
// pilot must see the evidence of each one to decide whether another reviewer
// is needed. While a flight completes, every committer turn qualifies, and so
// does a gate-failure reviewer's turn.
func notifiesEveryTurn(f Flight, role SessionRole) bool {
	switch f.WorkflowState {
	case StateExecuting:
		return role == RoleExecutor
	case StateReviewing:
		return role == RoleReviewer
	case StateCompleting:
		return role == RoleCommit || role == RoleReviewer
	}
	return false
}

func (m *Manager) notificationForTurn(f Flight, t MonitoredTurn, pilotID string, role SessionRole) *FlightMessage {
	turn := t.Turn
	always := notifiesEveryTurn(f, role)
	lines := []string{
		fmt.Sprintf("Flight %s; %s %s; records %d–%d; outcome %s", f.ID, role, turn.SessionID, turn.StartSeq, turn.EndSeq, turn.Outcome),
		"Conversation: " + chat.ConversationPath(schmuxdir.ChatSessionDir(f.Workspace.WorkspaceID, turn.SessionID)),
	}
	violation := false
	if role == RolePlanner {
		violation = t.Artifacts != nil && t.Artifacts.Error != ""
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
	}
	if x := t.Execution; role == RoleExecutor && x != nil {
		lines = append(lines, "Skill check: "+x.Skill)
		if x.ReportVerified {
			lines = append(lines, "Report check: verified: "+x.ReportPath, "Execution has finished: the report is verified. Compare the work in the workspace with the spec and plan; when it is complete and nothing is unresolved, request the review phase with an advance decision (\"advance\": \"reviewing\").")
		} else {
			lines = append(lines, "Report check: "+x.ReportError)
		}
	}
	if r := t.Review; role == RoleReviewer && r != nil {
		lines = append(lines, "Skill check: "+r.Skill)
	}
	if turn.Error != "" {
		lines = append(lines, "Harness error: "+turn.Error)
	}
	if turn.Outcome == "completed" {
		// A completed turn with no response text is noise, unless the daemon's
		// own checks found something in it: a placement violation or the
		// verified execution report.
		verified := t.Execution != nil && t.Execution.ReportVerified
		if strings.TrimSpace(turn.Text) == "" && !violation && !verified {
			return nil
		}
		// A placement violation always reaches the pilot; otherwise the
		// latest classification decides, unless every turn notifies.
		var last MonitorAttempt
		if len(t.Attempts) > 0 {
			last = t.Attempts[len(t.Attempts)-1]
		}
		classified := last.Config != nil && last.Classification != nil
		if !classified && !violation && !always {
			return nil
		}
		if classified {
			defs := last.Config.Roles[string(role)].Classifiers
			if !violation && !always && !shouldNotify(defs, last.Classification.Answers) {
				return nil
			}
			for _, def := range defs {
				lines = append(lines, "Question "+def.ID+": "+def.Question)
				for _, choice := range def.Choices {
					lines = append(lines, "  "+choice.ID+": "+choice.Description)
				}
			}
			for _, answer := range last.Classification.Answers {
				lines = append(lines, fmt.Sprintf("%s: %s (confidence %.2f)", answer.ID, answer.Choice, *answer.Confidence))
			}
		} else if last.Error != "" {
			lines = append(lines, "Classification failed: "+last.Error)
		}
	}
	lines = append(lines, pilotJobForRole(role), decisionContract, "Full response:", turn.Text)
	return &FlightMessage{Kind: "notification", SourceTurnID: t.ID, RecipientSessionID: pilotID, Text: strings.Join(lines, "\n"), Status: MessageStatusPending}
}

// pilotJobForRole is the pilot's job for the role that produced the turn. It
// arrives with every turn rather than in the takeoff prompt, so the pilot
// always knows what the current phase asks of it.
func pilotJobForRole(role SessionRole) string {
	switch role {
	case RolePlanner:
		return "Hold the planner to the planning steps: the spec and plan must exist, the coherence check must have run on the spec, and no question may be left open. Check the planner's conversation for evidence that a skill actually ran rather than trusting its claim. When planning is complete and no question is unresolved, start the execution phase with an advance decision (\"advance\": \"executing\"). Questions that need an answer and native permission prompts stay with the user. Do not implement, commit, edit the spec or plan, dispose the flight, or wipe the workspace."
	case RoleExecutor:
		return "Compare the work in the workspace with the spec and plan before relying on this response, and send the executor guidance (\"to\": \"executor\") about substantial drift. Check the executor's conversation for actual use of the execution skill rather than trusting its claim. When the verified report matches the work and nothing is unresolved, start the review phase with an advance decision (\"advance\": \"reviewing\"). Do not implement, commit, edit the spec or plan, dispose the flight, or wipe the workspace."
	case RoleReviewer:
		return "Hold the reviewer to its prompt. Inspect the reviewer's conversation for evidence rather than its claim: the test commands actually run with their output after the last change, and whether the test-rules-review skill was actually invoked. Send guidance to the reviewer (\"to\": \"reviewer\"). When a reviewer reports it changed something, ask for the next reviewer (\"next_reviewer\"). If the chain has gone off the rails, record a stop (\"stop\"). The review ends with the user: do not ask for completion and do not claim the user approved anything. Do not implement, commit, edit the spec or plan, dispose the flight, or wipe the workspace."
	case RoleCommit:
		return "Check the committer's response against the work in the workspace. When a commit gate fails, route the failure to a fresh reviewer with a next_reviewer decision (\"next_reviewer\") that carries the gate failure, and let the committer retry only after a reviewer changes nothing and everything it checked passed, and the user has approved the repaired work again. Send guidance to the committer (\"to\": \"commit\") or to the reviewer (\"to\": \"reviewer\"). Never supply the user's approval yourself. Do not implement, commit, edit the spec or plan, dispose the flight, or wipe the workspace."
	default:
		return "Check any claimed artifact in the workspace before relying on it."
	}
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
