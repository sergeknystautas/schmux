package detect

// jsonSettingsStrategy implements HookStrategy by merging schmux hooks into
// a JSON settings file (.claude/settings.local.json). Currently used by Claude.
type jsonSettingsStrategy struct{}

func init() {
	RegisterHookStrategy("json-settings-merge", &jsonSettingsStrategy{})
}

func (s *jsonSettingsStrategy) SupportsHooks() bool { return true }

func (s *jsonSettingsStrategy) SetupHooks(ctx HookContext) error {
	return claudeSetupHooks(ctx.WorkspacePath, ctx.HooksDir)
}

func (s *jsonSettingsStrategy) CleanupHooks(workspacePath string) error {
	return claudeCleanupHooks(workspacePath)
}

func (s *jsonSettingsStrategy) WrapRemoteCommand(command string) (string, error) {
	return claudeWrapRemoteCommand(command)
}
