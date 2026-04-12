//go:build nofloormanager

package floormanager

import (
	"context"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

type Manager struct{}

type Injector struct{}

func New(_ *config.Config, _ *session.Manager, _ *tmux.TmuxServer, _ string, _ *log.Logger) *Manager {
	return &Manager{}
}

func NewInjector(_ *Manager, _ int, _ *log.Logger) *Injector {
	return &Injector{}
}

func IsAvailable() bool { return false }

func (m *Manager) Start(_ context.Context) error { return nil }
func (m *Manager) Detach()                       {}
func (m *Manager) Stop()                         {}
func (m *Manager) TmuxSession() string           { return "" }
func (m *Manager) Tracker() *session.SessionRuntime {
	return nil
}
func (m *Manager) Running() bool                    { return false }
func (m *Manager) InjectionCount() int              { return 0 }
func (m *Manager) IncrementInjectionCount(int)      {}
func (m *Manager) ResetInjectionCount()             {}
func (m *Manager) EndShift()                        {}
func (m *Manager) HandleRotation(_ context.Context) {}

var _ events.EventHandler = (*Injector)(nil)

func (inj *Injector) HandleEvent(_ context.Context, _ string, _ events.RawEvent, _ []byte) {}
func (inj *Injector) Stop()                                                                {}
