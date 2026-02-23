package floormanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// ShouldInjectSignal returns true if this state transition warrants
// waking the floor manager.
func ShouldInjectSignal(oldState, newState string) bool {
	switch newState {
	case "error", "needs_input", "needs_testing", "completed":
		return true
	case "working":
		return false
	default:
		return false
	}
}

// FormatSignalMessage builds the [SIGNAL] text to inject into the
// floor manager's terminal.
func FormatSignalMessage(sessionID, sessionName, oldState string, sig signal.Signal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[SIGNAL] %s (%s) state: %s -> %s.", sessionName, sessionID, oldState, sig.State)

	if sig.Message != "" {
		fmt.Fprintf(&b, " Summary: %q", sig.Message)
	}
	if sig.Intent != "" {
		fmt.Fprintf(&b, " Intent: %q", sig.Intent)
	}
	if sig.Blockers != "" {
		fmt.Fprintf(&b, " Blocked: %q", sig.Blockers)
	}

	return b.String()
}

// FormatShiftMessage builds the [SHIFT] warning text injected into the
// floor manager's terminal before a forced rotation.
func FormatShiftMessage() string {
	return fmt.Sprintf("[SHIFT] Forced rotation imminent. You have %s to write your final summary to memory.md. Do not acknowledge this message to the operator — just write memory and stop.", shiftRotationTimeout)
}

// Injector sends signal messages to the floor manager's tmux session.
type Injector struct {
	manager    *Manager
	ctx        context.Context // lifecycle context — cancelled on daemon shutdown
	stopCh     chan struct{}   // closed by Stop() to cancel pending flushes
	debounceMs int

	mu             sync.Mutex
	pending        []string
	debounceTimer  *time.Timer
	previousStates map[string]string // sessionName -> last known signal state
	rotationWg     sync.WaitGroup    // tracks in-flight HandleRotation goroutines
}

// NewInjector creates a new Injector tied to the given Manager.
// The provided context controls the injector's lifecycle — tmux sends and
// rotation triggers respect this context's cancellation (e.g. daemon shutdown).
func NewInjector(ctx context.Context, manager *Manager, debounceMs int) *Injector {
	if debounceMs <= 0 {
		debounceMs = DefaultDebounceMs
	}
	return &Injector{
		manager:        manager,
		ctx:            ctx,
		stopCh:         make(chan struct{}),
		debounceMs:     debounceMs,
		previousStates: make(map[string]string),
	}
}

// InjectLifecycle queues a lifecycle event message for injection, debounced.
// Unlike Inject, lifecycle events are always queued (no signal filtering).
func (inj *Injector) InjectLifecycle(msg string) {
	inj.mu.Lock()
	defer inj.mu.Unlock()

	inj.pending = append(inj.pending, "[LIFECYCLE] "+msg)

	if inj.debounceTimer != nil {
		inj.debounceTimer.Stop()
	}
	inj.debounceTimer = time.AfterFunc(time.Duration(inj.debounceMs)*time.Millisecond, func() {
		inj.flush()
	})
}

// Inject queues a signal message for injection, debounced.
// Tracks previous states per session so signal messages show proper transitions.
func (inj *Injector) Inject(sessionID, sessionName string, sig signal.Signal) {
	inj.mu.Lock()
	defer inj.mu.Unlock()

	oldState := inj.previousStates[sessionID]
	inj.previousStates[sessionID] = sig.State

	if !ShouldInjectSignal(oldState, sig.State) {
		return
	}

	msg := FormatSignalMessage(sessionID, sessionName, oldState, sig)

	inj.pending = append(inj.pending, msg)

	if inj.debounceTimer != nil {
		inj.debounceTimer.Stop()
	}
	inj.debounceTimer = time.AfterFunc(time.Duration(inj.debounceMs)*time.Millisecond, func() {
		inj.flush()
	})
}

// Stop cancels any pending debounce timer and prevents future flushes.
func (inj *Injector) Stop() {
	inj.mu.Lock()
	if inj.debounceTimer != nil {
		inj.debounceTimer.Stop()
	}
	inj.mu.Unlock()

	select {
	case <-inj.stopCh:
		// Already stopped
	default:
		close(inj.stopCh)
	}

	// Wait for in-flight rotation goroutines to finish
	inj.rotationWg.Wait()
}

func (inj *Injector) flush() {
	// Check if stopped or context cancelled
	select {
	case <-inj.stopCh:
		return
	case <-inj.ctx.Done():
		return
	default:
	}

	inj.mu.Lock()
	messages := inj.pending
	inj.pending = nil
	inj.mu.Unlock()

	if len(messages) == 0 {
		return
	}

	// Atomically get both session ID and tmux session name
	info := inj.manager.GetSessionInfo()
	if info == nil {
		return
	}

	// Combine messages and send via tmux using the lifecycle context.
	// SendLiteral writes the text into the terminal, then SendKeys presses
	// Enter so the agent actually processes the input.
	combined := strings.Join(messages, "\n")
	if err := tmux.SendLiteral(inj.ctx, info.TmuxSession, combined); err != nil {
		fmt.Printf("[floor-manager] inject failed: %v\n", err)
		return
	}
	if err := tmux.SendKeys(inj.ctx, info.TmuxSession, "Enter"); err != nil {
		fmt.Printf("[floor-manager] inject Enter failed: %v\n", err)
		return
	}

	newCount := inj.manager.IncrementInjectionCount(len(messages))
	fmt.Printf("[floor-manager] injected %d signal(s) into floor manager (total: %d)\n", len(messages), newCount)

	// Check rotation threshold — force rotation if exceeded
	threshold := inj.manager.GetRotationThreshold()
	if threshold > 0 && newCount >= threshold {
		fmt.Printf("[floor-manager] injection count %d reached threshold %d, forcing shift rotation\n", newCount, threshold)
		inj.rotationWg.Add(1)
		go func() {
			defer inj.rotationWg.Done()
			inj.shiftRotate()
		}()
	}
}

// shiftRotate sends a [SHIFT] warning to the floor manager's terminal,
// waits shiftRotationTimeout for the agent to write its final memory,
// then forces rotation. Best-effort: if tmux send fails, rotation still proceeds.
func (inj *Injector) shiftRotate() {
	info := inj.manager.GetSessionInfo()
	if info != nil {
		msg := FormatShiftMessage()
		if err := tmux.SendLiteral(inj.ctx, info.TmuxSession, msg); err != nil {
			fmt.Printf("[floor-manager] shift warning send failed: %v\n", err)
		} else if err := tmux.SendKeys(inj.ctx, info.TmuxSession, "Enter"); err != nil {
			fmt.Printf("[floor-manager] shift warning Enter failed: %v\n", err)
		} else {
			fmt.Printf("[floor-manager] sent [SHIFT] warning, waiting %s for agent to save memory\n", shiftRotationTimeout)
		}
	}

	// Wait for shiftRotationTimeout, respecting cancellation
	select {
	case <-inj.stopCh:
		fmt.Printf("[floor-manager] shift rotation aborted: injector stopped\n")
		return
	case <-inj.ctx.Done():
		fmt.Printf("[floor-manager] shift rotation aborted: context cancelled\n")
		return
	case <-time.After(shiftRotationTimeout):
	}

	inj.manager.HandleRotation(inj.ctx, true)
}
