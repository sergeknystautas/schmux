import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';

type AdvancedTabProps = {
  loreEnabled: boolean;
  loreLLMTarget: string;
  loreCurateOnDispose: string;
  loreAutoPR: boolean;
  nudgenikTarget: string;
  viewedBuffer: number;
  nudgenikSeenInterval: number;
  desyncEnabled: boolean;
  desyncTarget: string;
  ioWorkspaceTelemetryEnabled: boolean;
  ioWorkspaceTelemetryTarget: string;
  branchSuggestTarget: string;
  conflictResolveTarget: string;
  soundDisabled: boolean;
  confirmBeforeClose: boolean;
  suggestDisposeAfterPush: boolean;
  dashboardPollInterval: number;
  gitStatusPollInterval: number;
  gitCloneTimeout: number;
  gitStatusTimeout: number;
  xtermQueryTimeout: number;
  xtermOperationTimeout: number;
  nudgenikTargetMissing: boolean;
  branchSuggestTargetMissing: boolean;
  conflictResolveTargetMissing: boolean;
  hasSaplingRepos: boolean;
  saplingCmdCreateWorkspace: string;
  saplingCmdRemoveWorkspace: string;
  saplingCmdCheckRepoBase: string;
  saplingCmdCreateRepoBase: string;
  tmuxBinary: string;
  stepErrors: Record<number, string | null>;
  models: Model[];
  dispatch: React.Dispatch<ConfigFormAction>;
};

export default function AdvancedTab({
  loreEnabled,
  loreLLMTarget,
  loreCurateOnDispose,
  loreAutoPR,
  nudgenikTarget,
  viewedBuffer,
  nudgenikSeenInterval,
  desyncEnabled,
  desyncTarget,
  ioWorkspaceTelemetryEnabled,
  ioWorkspaceTelemetryTarget,
  branchSuggestTarget,
  conflictResolveTarget,
  soundDisabled,
  confirmBeforeClose,
  suggestDisposeAfterPush,
  dashboardPollInterval,
  gitStatusPollInterval,
  gitCloneTimeout,
  gitStatusTimeout,
  xtermQueryTimeout,
  xtermOperationTimeout,
  nudgenikTargetMissing,
  branchSuggestTargetMissing,
  conflictResolveTargetMissing,
  hasSaplingRepos,
  saplingCmdCreateWorkspace,
  saplingCmdRemoveWorkspace,
  saplingCmdCheckRepoBase,
  saplingCmdCreateRepoBase,
  tmuxBinary,
  stepErrors,
  models,
  dispatch,
}: AdvancedTabProps) {
  const setField = (field: string, value: unknown) =>
    dispatch({
      type: 'SET_FIELD',
      field: field as keyof import('./useConfigForm').ConfigFormState,
      value,
    });

  return (
    <div className="wizard-step-content" data-step="6" data-testid="config-tab-content-advanced">
      <h2 className="wizard-step-content__title">Advanced Settings</h2>
      <p className="wizard-step-content__description">
        Terminal dimensions and advanced timing controls. You can leave these as defaults unless you
        have specific needs.
      </p>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Lore</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={loreEnabled}
                onChange={(e) => setField('loreEnabled', e.target.checked)}
              />
              <span>Enable lore system</span>
            </label>
            <p className="form-group__hint">
              A system that continuously learns about where agents make mistakes and automatically
              turns them into suggestions for updates to their instructions.
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label" htmlFor="lore-llm-target">
              LLM Target
            </label>
            <TargetSelect
              id="lore-llm-target"
              value={loreLLMTarget}
              onChange={(v) => setField('loreLLMTarget', v)}
              disabled={!loreEnabled}
              includeDisabledOption={false}
              includeNoneOption="None (curator disabled)"
              models={models}
            />
            <p className="form-group__hint">
              Promptable target for curating lore entries into documentation proposals.
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label" htmlFor="lore-curate-on-dispose">
              Curate On Dispose
            </label>
            <select
              id="lore-curate-on-dispose"
              className="input"
              value={loreCurateOnDispose}
              onChange={(e) => setField('loreCurateOnDispose', e.target.value)}
              disabled={!loreEnabled}
            >
              <option value="session">Every session</option>
              <option value="workspace">Last session per workspace</option>
              <option value="never">Never (manual only)</option>
            </select>
            <p className="form-group__hint">
              When to automatically trigger lore curation after disposing a session.
            </p>
          </div>

          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={loreAutoPR}
                onChange={(e) => setField('loreAutoPR', e.target.checked)}
                disabled={!loreEnabled}
              />
              <span>Auto-create PR after applying proposals</span>
            </label>
            <p className="form-group__hint">
              Automatically open a pull request when a lore proposal is applied.
            </p>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">NudgeNik</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={nudgenikTarget}
              onChange={(v) => setField('nudgenikTarget', v)}
              models={models}
            />
            <p className="form-group__hint">
              Select a model for NudgeNik session feedback, or leave disabled.
            </p>
            {nudgenikTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>

          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Viewed Buffer (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={viewedBuffer === 0 ? '' : viewedBuffer}
                onChange={(e) =>
                  setField(
                    'viewedBuffer',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                  )
                }
              />
              <p className="form-group__hint">
                Time to keep session marked as "viewed" after last check
              </p>
            </div>

            <div className="form-group">
              <label className="form-group__label">Seen Interval (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={nudgenikSeenInterval === 0 ? '' : nudgenikSeenInterval}
                onChange={(e) =>
                  setField(
                    'nudgenikSeenInterval',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 2000
                  )
                }
              />
              <p className="form-group__hint">How often to check for session activity</p>
            </div>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Terminal Desync Diagnostics</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={desyncEnabled}
                onChange={(e) => setField('desyncEnabled', e.target.checked)}
              />
              Enable terminal desync diagnostics
            </label>
            <p className="form-group__hint">
              When enabled, the terminal viewer shows pipeline metrics and a capture button to
              diagnose visual discrepancies between tmux and xterm.js.
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={desyncTarget}
              onChange={(v) => setField('desyncTarget', v)}
              disabled={!desyncEnabled}
              includeDisabledOption={false}
              includeNoneOption="None (capture only)"
              models={models}
            />
            <p className="form-group__hint">
              When a target is selected, a diagnostic capture will automatically spawn an agent
              session to analyze the captured data. Leave as &quot;None&quot; to capture files
              without spawning an agent.
            </p>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">IO Workspace Telemetry</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={ioWorkspaceTelemetryEnabled}
                onChange={(e) => setField('ioWorkspaceTelemetryEnabled', e.target.checked)}
              />
              Enable IO workspace telemetry
            </label>
            <p className="form-group__hint">
              When enabled, workspace git operations are instrumented with timing telemetry.
            </p>
          </div>

          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={ioWorkspaceTelemetryTarget}
              onChange={(v) => setField('ioWorkspaceTelemetryTarget', v)}
              disabled={!ioWorkspaceTelemetryEnabled}
              includeDisabledOption={false}
              includeNoneOption="None (capture only)"
              models={models}
            />
            <p className="form-group__hint">
              When a target is selected, a diagnostic capture will automatically spawn an agent
              session to analyze the captured data. Leave as &quot;None&quot; to capture files
              without spawning an agent.
            </p>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Branch Suggestion</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={branchSuggestTarget}
              onChange={(v) => setField('branchSuggestTarget', v)}
              models={models}
            />
            <p className="form-group__hint">
              Select a model for branch name suggestion, or leave disabled.
            </p>
            {branchSuggestTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Conflict Resolution</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label">Target</label>
            <TargetSelect
              value={conflictResolveTarget}
              onChange={(v) => setField('conflictResolveTarget', v)}
              models={models}
            />
            <p className="form-group__hint">
              Select a model for merge conflict resolution. When &quot;sync from main conflict&quot;
              encounters a conflict, this target will be spawned to resolve it.
            </p>
            {conflictResolveTargetMissing && (
              <p className="form-group__error">Selected target is not available.</p>
            )}
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Notifications</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={!soundDisabled}
                onChange={(e) => setField('soundDisabled', !e.target.checked)}
              />
              <span>Play sound when agents need attention</span>
            </label>
            <p className="form-group__hint">
              Plays an audio notification when an agent signals it needs input or encounters an
              error.
            </p>
          </div>
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={confirmBeforeClose}
                onChange={(e) => setField('confirmBeforeClose', e.target.checked)}
              />
              <span>Confirm before closing tab</span>
            </label>
            <p className="form-group__hint">
              Shows a browser &ldquo;Leave site?&rdquo; dialog when closing or reloading the
              dashboard tab.
            </p>
          </div>
          <div className="form-group">
            <label
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 'var(--spacing-xs)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={suggestDisposeAfterPush}
                onChange={(e) => setField('suggestDisposeAfterPush', e.target.checked)}
              />
              <span>Suggest disposing workspace after push to main</span>
            </label>
            <p className="form-group__hint">
              After pushing all commits to main, prompts to dispose the workspace and its sessions.
            </p>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">tmux</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="form-group__label" htmlFor="tmux-binary">
              Binary Path
            </label>
            <input
              id="tmux-binary"
              type="text"
              className="input"
              placeholder="tmux (from $PATH)"
              value={tmuxBinary}
              onChange={(e) => setField('tmuxBinary', e.target.value)}
            />
            <p className="form-group__hint">
              Path to a custom tmux binary. Leave empty to use the system default from $PATH. The
              path is validated on save. Changes require a daemon restart.
            </p>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Sessions</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Dashboard Poll Interval (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={dashboardPollInterval === 0 ? '' : dashboardPollInterval}
                onChange={(e) =>
                  setField(
                    'dashboardPollInterval',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                  )
                }
              />
              <p className="form-group__hint">How often to refresh sessions list</p>
            </div>

            <div className="form-group">
              <label className="form-group__label">Git Status Poll Interval (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={gitStatusPollInterval === 0 ? '' : gitStatusPollInterval}
                onChange={(e) =>
                  setField(
                    'gitStatusPollInterval',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 10000
                  )
                }
              />
              <p className="form-group__hint">
                How often to refresh git status (dirty, ahead, behind)
              </p>
            </div>
          </div>

          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Git Clone Timeout (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={gitCloneTimeout === 0 ? '' : gitCloneTimeout}
                onChange={(e) =>
                  setField(
                    'gitCloneTimeout',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 300000
                  )
                }
              />
              <p className="form-group__hint">
                Maximum time to wait for git clone when spawning sessions (default: 300000ms = 5
                min)
              </p>
            </div>

            <div className="form-group">
              <label className="form-group__label">Git Status Timeout (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={gitStatusTimeout === 0 ? '' : gitStatusTimeout}
                onChange={(e) =>
                  setField(
                    'gitStatusTimeout',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 30000
                  )
                }
              />
              <p className="form-group__hint">
                Maximum time to wait for git status/diff operations (default: 30000ms)
              </p>
            </div>
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Xterm</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Query Timeout (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={xtermQueryTimeout === 0 ? '' : xtermQueryTimeout}
                onChange={(e) =>
                  setField(
                    'xtermQueryTimeout',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 5000
                  )
                }
              />
              <p className="form-group__hint">
                Maximum time to wait for xterm query operations (default: 5000ms)
              </p>
            </div>

            <div className="form-group">
              <label className="form-group__label">Operation Timeout (ms)</label>
              <input
                type="number"
                className="input input--compact"
                min="100"
                value={xtermOperationTimeout === 0 ? '' : xtermOperationTimeout}
                onChange={(e) =>
                  setField(
                    'xtermOperationTimeout',
                    e.target.value === '' ? 0 : parseInt(e.target.value) || 10000
                  )
                }
              />
              <p className="form-group__hint">
                Maximum time to wait for xterm operations (default: 10000ms)
              </p>
            </div>
          </div>
        </div>
      </div>

      {hasSaplingRepos && (
        <div className="settings-section">
          <div className="settings-section__header">
            <h3 className="settings-section__title">Sapling Commands</h3>
          </div>
          <div className="settings-section__body">
            <p className="form-group__hint" style={{ marginBottom: 'var(--spacing-md)' }}>
              Command templates for sapling workspace lifecycle. Uses Go text/template syntax.
            </p>
            {[
              {
                field: 'saplingCmdCreateWorkspace' as const,
                label: 'Create Workspace',
                placeholder: 'sl clone {{.RepoIdentifier}} {{.DestPath}}',
                value: saplingCmdCreateWorkspace,
              },
              {
                field: 'saplingCmdRemoveWorkspace' as const,
                label: 'Remove Workspace',
                placeholder: 'rm -rf {{.WorkspacePath}}',
                value: saplingCmdRemoveWorkspace,
              },
              {
                field: 'saplingCmdCreateRepoBase' as const,
                label: 'Create Repo Base',
                placeholder: 'sl clone {{.RepoIdentifier}} {{.BasePath}}',
                value: saplingCmdCreateRepoBase,
              },
              {
                field: 'saplingCmdCheckRepoBase' as const,
                label: 'Check Repo Base',
                placeholder: '',
                value: saplingCmdCheckRepoBase,
              },
            ].map(({ field, label, placeholder, value }) => (
              <div className="form-group" key={field}>
                <label className="form-group__label">{label}</label>
                <input
                  type="text"
                  className="input"
                  placeholder={placeholder}
                  value={value}
                  onChange={(e) => dispatch({ type: 'SET_FIELD', field, value: e.target.value })}
                />
              </div>
            ))}
          </div>
        </div>
      )}

      {stepErrors[5] && <p className="form-group__error">{stepErrors[5]}</p>}
    </div>
  );
}
