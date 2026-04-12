import React from 'react';
import TargetSelect from './TargetSelect';
import type { ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';

type AdvancedTabProps = {
  desyncEnabled: boolean;
  desyncTarget: string;
  ioWorkspaceTelemetryEnabled: boolean;
  ioWorkspaceTelemetryTarget: string;
  dashboardPollInterval: number;
  gitStatusPollInterval: number;
  gitCloneTimeout: number;
  gitStatusTimeout: number;
  xtermQueryTimeout: number;
  xtermOperationTimeout: number;
  xtermUseWebGL: boolean;
  localEchoRemote: boolean;
  debugUI: boolean;
  hasSaplingRepos: boolean;
  saplingCmdCreateWorkspace: string;
  saplingCmdRemoveWorkspace: string;
  saplingCmdCheckRepoBase: string;
  saplingCmdCreateRepoBase: string;
  tmuxBinary: string;
  tmuxSocketName: string;
  externalDiffCommands: { name: string; command: string }[];
  externalDiffCleanupMinutes: number;
  newDiffName: string;
  newDiffCommand: string;
  onAddDiffCommand: () => void;
  models: Model[];
  dispatch: React.Dispatch<ConfigFormAction>;
};

export default function AdvancedTab({
  desyncEnabled,
  desyncTarget,
  ioWorkspaceTelemetryEnabled,
  ioWorkspaceTelemetryTarget,
  dashboardPollInterval,
  gitStatusPollInterval,
  gitCloneTimeout,
  gitStatusTimeout,
  xtermQueryTimeout,
  xtermOperationTimeout,
  xtermUseWebGL,
  localEchoRemote,
  debugUI,
  hasSaplingRepos,
  saplingCmdCreateWorkspace,
  saplingCmdRemoveWorkspace,
  saplingCmdCheckRepoBase,
  saplingCmdCreateRepoBase,
  tmuxBinary,
  tmuxSocketName,
  externalDiffCommands,
  externalDiffCleanupMinutes,
  newDiffName,
  newDiffCommand,
  onAddDiffCommand,
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
          <h3 className="settings-section__title">Debug</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-group">
            <label className="flex-row gap-xs cursor-pointer">
              <input
                type="checkbox"
                checked={debugUI}
                onChange={(e) => setField('debugUI', e.target.checked)}
              />
              <span>Enable debug UI</span>
            </label>
            <p className="form-group__hint">
              Show diagnostic panels and tools in the sidebar without running in dev mode. Enables
              Event Monitor, Tmux diagnostics, Typing Performance, Autolearn Curation status, remote
              access simulation, and debug API endpoints. Takes effect immediately.
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

          <div className="form-group">
            <label className="form-group__label" htmlFor="tmux-socket-name">
              Socket Name
            </label>
            <input
              id="tmux-socket-name"
              type="text"
              className="input"
              placeholder="schmux (default)"
              value={tmuxSocketName}
              onChange={(e) => setField('tmuxSocketName', e.target.value)}
            />
            <p className="form-group__hint">
              &quot;schmux&quot; = isolated server (recommended), &quot;default&quot; = shared with
              your tmux sessions
            </p>
            <p className="form-group__hint">
              Takes effect for new sessions. Existing sessions continue on their current socket.
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

          <div className="form-group">
            <label className="flex-row gap-xs cursor-pointer">
              <input
                type="checkbox"
                checked={xtermUseWebGL}
                onChange={(e) => setField('xtermUseWebGL', e.target.checked)}
              />
              <span>Use WebGL renderer</span>
            </label>
            <p className="form-group__hint">
              Uses GPU-accelerated WebGL rendering for the terminal. Disable to fall back to the
              canvas renderer if you experience visual glitches.
            </p>
          </div>

          <div className="form-group">
            <label className="flex-row gap-xs cursor-pointer">
              <input
                type="checkbox"
                checked={localEchoRemote}
                onChange={(e) => setField('localEchoRemote', e.target.checked)}
              />
              <span>Enable local echo for remote sessions</span>
            </label>
            <p className="form-group__hint">
              Automatically enables local echo (native typing) when opening a remote session,
              reducing perceived input latency. You can still toggle it per-session.
            </p>
          </div>
        </div>
      </div>

      {/* Custom Diff Tools (absorbed from CodeReviewTab) */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Custom Diff Tools</h3>
        </div>
        <div className="settings-section__body">
          {externalDiffCommands.length === 0 ? (
            <div className="empty-state-hint">No custom diff tools configured.</div>
          ) : (
            <div className="item-list item-list--two-col">
              {externalDiffCommands.map((cmd) => (
                <div className="item-list__item" key={cmd.name}>
                  <div className="item-list__item-primary item-list__item-row">
                    <span className="item-list__item-name">{cmd.name}</span>
                    <span className="item-list__item-detail item-list__item-detail--wide mono">
                      {cmd.command}
                    </span>
                  </div>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => dispatch({ type: 'REMOVE_DIFF_COMMAND', name: cmd.name })}
                  >
                    Remove
                  </button>
                </div>
              ))}
            </div>
          )}

          <h3>Add Custom Diff Tool</h3>
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Name</label>
              <input
                type="text"
                className="input"
                placeholder="e.g., Kaleidoscope"
                value={newDiffName}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'newDiffName', value: e.target.value })
                }
              />
            </div>
            <div className="form-group">
              <label className="form-group__label">Command</label>
              <input
                type="text"
                className="input"
                placeholder="e.g., ksdiff"
                value={newDiffCommand}
                onChange={(e) =>
                  dispatch({ type: 'SET_FIELD', field: 'newDiffCommand', value: e.target.value })
                }
              />
            </div>
            <div
              style={{
                display: 'flex',
                alignItems: 'flex-end',
                paddingBottom: 'var(--spacing-sm)',
              }}
            >
              <button
                type="button"
                className="btn btn--primary"
                disabled={!newDiffName.trim() || !newDiffCommand.trim()}
                onClick={onAddDiffCommand}
              >
                Add Diff Tool
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Temp Cleanup (absorbed from CodeReviewTab) */}
      <div className="settings-section">
        <div className="settings-section__header">
          <h3 className="settings-section__title">Temp Cleanup</h3>
        </div>
        <div className="settings-section__body">
          <div className="form-row">
            <div className="form-group">
              <label className="form-group__label">Cleanup after (minutes)</label>
              <input
                type="number"
                min="1"
                className="input"
                value={externalDiffCleanupMinutes}
                onChange={(e) =>
                  dispatch({
                    type: 'SET_FIELD',
                    field: 'externalDiffCleanupMinutes',
                    value: Math.max(1, Number(e.target.value) || 1),
                  })
                }
              />
              <p className="form-group__hint">
                Temp diff files will be removed after this delay (default: 60 minutes).
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
            <p className="form-group__hint mb-md">
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

      {/* Dev-only features — only visible when debug_ui is on */}
      {debugUI && (
        <>
          <div className="settings-section">
            <div className="settings-section__header">
              <h3 className="settings-section__title">Terminal Desync Diagnostics</h3>
            </div>
            <div className="settings-section__body">
              <div className="form-group">
                <label className="flex-row gap-xs cursor-pointer">
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
                <label className="flex-row gap-xs cursor-pointer">
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
        </>
      )}
    </div>
  );
}
