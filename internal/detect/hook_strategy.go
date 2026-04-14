package detect

import "fmt"

// HookStrategy defines how schmux injects lifecycle hooks into an agent's
// configuration. The strategy handles format-specific setup, cleanup, and
// remote command wrapping.
type HookStrategy interface {
	SupportsHooks() bool
	SetupHooks(ctx HookContext) error
	CleanupHooks(workspacePath string) error
	WrapRemoteCommand(command string) (string, error)
}

var hookStrategies = map[string]HookStrategy{
	"none": &noneHookStrategy{},
	"":     &noneHookStrategy{},
}

// GetHookStrategy returns the hook strategy registered under the given name.
func GetHookStrategy(name string) (HookStrategy, error) {
	s, ok := hookStrategies[name]
	if !ok {
		return nil, fmt.Errorf("unknown hook strategy: %q", name)
	}
	return s, nil
}

// RegisterHookStrategy registers a hook strategy by name.
func RegisterHookStrategy(name string, s HookStrategy) {
	hookStrategies[name] = s
}

type noneHookStrategy struct{}

func (s *noneHookStrategy) SupportsHooks() bool                          { return false }
func (s *noneHookStrategy) SetupHooks(_ HookContext) error               { return nil }
func (s *noneHookStrategy) CleanupHooks(_ string) error                  { return nil }
func (s *noneHookStrategy) WrapRemoteCommand(cmd string) (string, error) { return cmd, nil }
