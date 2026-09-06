import Tooltip from './Tooltip';
import { formatRelativeTime, formatTimestamp } from '../lib/utils';
import type { SessionResponse, SessionWithWorkspace } from '../lib/types';
import type { ConfigResponse } from '../lib/types.generated';

interface SessionSidebarProps {
  session: SessionResponse & Pick<SessionWithWorkspace, 'model'>;
  config: Pick<ConfigResponse, 'tmux_socket_name' | 'system_capabilities'>;
  /** Attach command and iTerm2 link: terminal sessions only. */
  showAttach: boolean;
  onEditNickname: () => void;
  onCopyAttach: () => void;
  onDispose: () => void;
}

/**
 * The right column of a session page: session metadata, nickname, persona,
 * timestamps, the attach command (terminal sessions), and Dispose. Shared by
 * the terminal and chat pages so both render the same column.
 */
export default function SessionSidebar({
  session: sessionData,
  config,
  showAttach,
  onEditNickname,
  onCopyAttach,
  onDispose,
}: SessionSidebarProps) {
  return (
    <aside
      className="session-detail__sidebar"
      data-tour="session-detail-sidebar"
      data-testid="session-sidebar"
    >
      <div className="metadata-field">
        <span className="metadata-field__label">Session ID</span>
        <span className="metadata-field__value metadata-field__value--mono">{sessionData.id}</span>
      </div>

      <div className="metadata-field">
        <span className="metadata-field__label">Target</span>
        <span className="metadata-field__value">{sessionData.target}</span>
      </div>

      {sessionData.model && sessionData.model.context_window ? (
        <div className="metadata-field">
          <span className="metadata-field__label">Context Window</span>
          <span className="metadata-field__value">
            {(sessionData.model.context_window / 1000).toFixed(0)}K tokens
          </span>
        </div>
      ) : null}
      {sessionData.model &&
      (sessionData.model.cost_input_per_mtok || sessionData.model.cost_output_per_mtok) ? (
        <div className="metadata-field">
          <span className="metadata-field__label">Pricing</span>
          <span className="metadata-field__value">
            ${sessionData.model.cost_input_per_mtok || 0} / $
            {sessionData.model.cost_output_per_mtok || 0} per MTok
          </span>
        </div>
      ) : null}

      {sessionData.nickname ? (
        <div className="metadata-field" data-testid="session-nickname">
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              width: '100%',
            }}
          >
            <span className="metadata-field__label">Nickname</span>
            <Tooltip content="Edit nickname">
              <button className="btn btn--sm btn--ghost" onClick={onEditNickname}>
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                  <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                </svg>
              </button>
            </Tooltip>
          </div>
          <span className="metadata-field__value">{sessionData.nickname}</span>
        </div>
      ) : (
        <div className="metadata-field" data-testid="session-nickname">
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              width: '100%',
            }}
          >
            <span className="metadata-field__label">Nickname</span>
            <Tooltip content="Add nickname">
              <button className="btn btn--sm btn--ghost" onClick={onEditNickname}>
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <line x1="12" y1="5" x2="12" y2="19"></line>
                  <line x1="5" y1="12" x2="19" y2="12"></line>
                </svg>
              </button>
            </Tooltip>
          </div>
          <span
            className="metadata-field__value"
            style={{ color: 'var(--color-text-muted)', fontStyle: 'italic' }}
          >
            Not set
          </span>
        </div>
      )}

      {sessionData.persona_id && (
        <div className="metadata-field">
          <span className="metadata-field__label">Persona</span>
          <span className="metadata-field__value">
            {sessionData.persona_icon && (
              <span style={{ color: sessionData.persona_color, marginRight: '4px' }}>
                {sessionData.persona_icon}
              </span>
            )}
            {sessionData.persona_name || sessionData.persona_id}
          </span>
        </div>
      )}

      <div className="metadata-field">
        <span className="metadata-field__label">Created</span>
        <Tooltip content={formatTimestamp(sessionData.created_at)}>
          <span className="metadata-field__value" style={{ alignSelf: 'flex-start' }}>
            {formatRelativeTime(sessionData.created_at)}
          </span>
        </Tooltip>
      </div>

      <div className="metadata-field">
        <span className="metadata-field__label">Last Activity</span>
        <Tooltip
          content={
            sessionData.last_output_at ? formatTimestamp(sessionData.last_output_at) : 'Never'
          }
        >
          <span className="metadata-field__value" style={{ alignSelf: 'flex-start' }}>
            {sessionData.last_output_at ? formatRelativeTime(sessionData.last_output_at) : 'Never'}
          </span>
        </Tooltip>
      </div>

      {sessionData.tmux_socket &&
        sessionData.tmux_socket !== (config.tmux_socket_name || 'schmux') && (
          <div className="metadata-field">
            <span className="metadata-field__label">Socket</span>
            <span
              className="metadata-field__value metadata-field__value--mono"
              style={{ fontSize: '0.75rem' }}
            >
              {sessionData.tmux_socket}
            </span>
          </div>
        )}

      {sessionData.remote_host_id && (
        <>
          <hr
            style={{
              border: 'none',
              borderTop: '1px solid var(--color-border)',
              margin: 'var(--spacing-md) 0',
            }}
          />
          <div className="metadata-field">
            <span className="metadata-field__label">Environment</span>
            <span className="metadata-field__value flex-row gap-xs">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
                <line x1="1" y1="10" x2="23" y2="10" />
              </svg>
              {sessionData.remote_flavor_name || 'Remote'}
            </span>
          </div>
          {sessionData.remote_hostname && (
            <div className="metadata-field">
              <span className="metadata-field__label">Hostname</span>
              <span
                className="metadata-field__value metadata-field__value--mono"
                style={{ fontSize: '0.75rem' }}
              >
                {sessionData.remote_hostname}
              </span>
            </div>
          )}
        </>
      )}

      {showAttach && (
        <>
          <hr
            style={{
              border: 'none',
              borderTop: '1px solid var(--color-border)',
              margin: 'var(--spacing-md) 0',
            }}
          />

          <div className="form-group">
            <label className="form-group__label">Attach Command</label>
            <div className="copy-field">
              <span className="copy-field__value">{sessionData.attach_cmd}</span>
              <Tooltip content="Copy attach command">
                <button className="copy-field__btn" onClick={onCopyAttach}>
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                  </svg>
                </button>
              </Tooltip>
            </div>
          </div>

          {!sessionData.remote_host_id && config.system_capabilities?.iterm2_available && (
            <div className="form-group">
              <Tooltip content="Open tmux session in iTerm2">
                <a
                  className="iterm2-link"
                  href={`iterm2:///command?c=${encodeURIComponent(sessionData.attach_cmd)}`}
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                    <polyline points="15 3 21 3 21 9" />
                    <line x1="10" y1="14" x2="21" y2="3" />
                  </svg>
                  <span>Open in iTerm2</span>
                </a>
              </Tooltip>
            </div>
          )}
        </>
      )}

      <div style={{ marginTop: 'auto' }}>
        <button
          className="btn btn--danger"
          style={{ width: '100%' }}
          onClick={onDispose}
          disabled={sessionData.status === 'disposing'}
          data-testid="dispose-session"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          </svg>
          Dispose Session
        </button>
      </div>
    </aside>
  );
}
