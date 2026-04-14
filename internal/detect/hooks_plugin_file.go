package detect

// pluginFileStrategy implements HookStrategy by writing a TypeScript plugin
// file into the agent's plugin directory. Currently used by OpenCode.
type pluginFileStrategy struct{}

func init() {
	RegisterHookStrategy("plugin-file", &pluginFileStrategy{})
}

func (s *pluginFileStrategy) SupportsHooks() bool { return true }

func (s *pluginFileStrategy) SetupHooks(ctx HookContext) error {
	return opencodeSetupHooks(ctx.WorkspacePath)
}

func (s *pluginFileStrategy) CleanupHooks(workspacePath string) error {
	return opencodeCleanupHooks(workspacePath)
}

func (s *pluginFileStrategy) WrapRemoteCommand(command string) (string, error) {
	return opencodeWrapRemoteCommand(command)
}
