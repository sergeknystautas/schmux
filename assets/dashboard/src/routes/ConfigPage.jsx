import React, { useEffect, useState, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { getConfig, updateConfig, getSessions, getWorkspaces } from '../lib/api.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';

export default function ConfigPage() {
  const [searchParams] = useSearchParams();
  const isFirstRun = searchParams.get('firstRun') === 'true';
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [warning, setWarning] = useState('');
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();

  // Form state
  const [workspacePath, setWorkspacePath] = useState('');
  const [terminalWidth, setTerminalWidth] = useState('120');
  const [terminalHeight, setTerminalHeight] = useState('40');
  const [terminalSeedLines, setTerminalSeedLines] = useState('100');
  const [repos, setRepos] = useState([]);
  const [agents, setAgents] = useState([]);

  // Terminal input refs for uncontrolled inputs
  const terminalWidthRef = useRef(null);
  const terminalHeightRef = useRef(null);
  const terminalSeedLinesRef = useRef(null);

  // Input states for new items
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoUrl, setNewRepoUrl] = useState('');
  const [newAgentName, setNewAgentName] = useState('');
  const [newAgentCommand, setNewAgentCommand] = useState('');

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getConfig();
        if (!active) return;
        setConfig(data);
        setWorkspacePath(data.workspace_path || '');
        setTerminalWidth(String(data.terminal?.width || 120));
        setTerminalHeight(String(data.terminal?.height || 40));
        setTerminalSeedLines(String(data.terminal?.seed_lines || 100));
        setRepos(data.repos || []);
        setAgents(data.agents || []);
      } catch (err) {
        if (!active) return;
        setError(err.message || 'Failed to load config');
      } finally {
        if (active) setLoading(false);
      }
    };

    load();
    return () => { active = false; };
  }, []);

  const handleSave = async () => {
    // Get terminal values from uncontrolled inputs
    const width = parseInt(terminalWidthRef.current?.value || '0');
    const height = parseInt(terminalHeightRef.current?.value || '0');
    const seedLines = parseInt(terminalSeedLinesRef.current?.value || '0');

    // Validate
    if (!workspacePath.trim()) {
      toastError('Workspace path is required');
      return;
    }
    if (width <= 0) {
      toastError('Terminal width must be greater than 0');
      return;
    }
    if (height <= 0) {
      toastError('Terminal height must be greater than 0');
      return;
    }
    if (seedLines <= 0) {
      toastError('Terminal seed lines must be greater than 0');
      return;
    }

    // Check for duplicate repo names
    const repoNames = new Set();
    for (const repo of repos) {
      if (repoNames.has(repo.name)) {
        toastError(`Duplicate repo name: ${repo.name}`);
        return;
      }
      repoNames.add(repo.name);
    }

    // Check for duplicate agent names
    const agentNames = new Set();
    for (const agent of agents) {
      if (agentNames.has(agent.name)) {
        toastError(`Duplicate agent name: ${agent.name}`);
        return;
      }
      agentNames.add(agent.name);
    }

    setSaving(true);
    setWarning('');

    try {
      const updateRequest = {
        workspace_path: workspacePath,
        terminal: {
          width: width,
          height: height,
          seed_lines: seedLines
        },
        repos: repos,
        agents: agents
      };

      const result = await updateConfig(updateRequest);

      // Check if there's a warning about workspace path change
      if (result.warning) {
        setWarning(result.warning);
      } else {
        success('Config saved and reloaded. Changes are now in effect.');
      }
    } catch (err) {
      toastError(err.message || 'Failed to save config');
    } finally {
      setSaving(false);
    }
  };

  const addRepo = () => {
    if (!newRepoName.trim()) {
      toastError('Repo name is required');
      return;
    }
    if (!newRepoUrl.trim()) {
      toastError('Repo URL is required');
      return;
    }
    if (repos.some(r => r.name === newRepoName)) {
      toastError('Repo name already exists');
      return;
    }
    setRepos([...repos, { name: newRepoName, url: newRepoUrl }]);
    setNewRepoName('');
    setNewRepoUrl('');
  };

  const removeRepo = async (name) => {
    const confirmed = await confirm('Remove repo?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setRepos(repos.filter(r => r.name !== name));
    }
  };

  const addAgent = () => {
    if (!newAgentName.trim()) {
      toastError('Agent name is required');
      return;
    }
    if (!newAgentCommand.trim()) {
      toastError('Agent command is required');
      return;
    }
    if (agents.some(a => a.name === newAgentName)) {
      toastError('Agent name already exists');
      return;
    }
    setAgents([...agents, { name: newAgentName, command: newAgentCommand }]);
    setNewAgentName('');
    setNewAgentCommand('');
  };

  const removeAgent = async (name) => {
    const confirmed = await confirm('Remove agent?', `Remove "${name}" from the config?`);
    if (confirmed) {
      setAgents(agents.filter(a => a.name !== name));
    }
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading configuration...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Failed to load config</h3>
        <p className="empty-state__description">{error}</p>
      </div>
    );
  }

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Configuration</h1>
      </div>

      {isFirstRun && (
        <div className="banner banner--info" style={{ marginBottom: 'var(--spacing-lg)' }}>
          <p style={{ margin: 0 }}>
            <strong>Welcome to schmux!</strong> Before you can start spawning sessions, you need to configure at least one repository and one agent. Fill in the details below and click <strong>Save Configuration</strong> when you're done.
          </p>
        </div>
      )}

      {warning && (
        <div className="banner banner--warning" style={{ marginBottom: 'var(--spacing-lg)' }}>
          <p style={{ margin: 0 }}>
            <strong>Warning:</strong> {warning}
          </p>
        </div>
      )}

      <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
        <div className="card__header">
          <h2 className="card__title">Terminal Settings</h2>
        </div>
        <div className="card__body">
          <div style={{ display: 'flex', gap: 'var(--spacing-md)', flexWrap: 'wrap' }}>
            <div className="form-group" style={{ flex: '1', minWidth: '150px', marginBottom: 0 }}>
              <label className="form-group__label">Width</label>
              <input
                ref={terminalWidthRef}
                type="number"
                className="input"
                min="1"
                defaultValue={terminalWidth}
              />
              <p className="form-group__hint">Terminal width in columns</p>
            </div>

            <div className="form-group" style={{ flex: '1', minWidth: '150px', marginBottom: 0 }}>
              <label className="form-group__label">Height</label>
              <input
                ref={terminalHeightRef}
                type="number"
                className="input"
                min="1"
                defaultValue={terminalHeight}
              />
              <p className="form-group__hint">Terminal height in rows</p>
            </div>

            <div className="form-group" style={{ flex: '1', minWidth: '150px', marginBottom: 0 }}>
              <label className="form-group__label">Seed Lines</label>
              <input
                ref={terminalSeedLinesRef}
                type="number"
                className="input"
                min="1"
                defaultValue={terminalSeedLines}
              />
              <p className="form-group__hint">Lines to capture when reconnecting</p>
            </div>
          </div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
        <div className="card__header">
          <h2 className="card__title">Workspace Settings</h2>
        </div>
        <div className="card__body">
          <div className="form-group">
            <label className="form-group__label">Workspace Path</label>
            <input
              type="text"
              className="input"
              value={workspacePath}
              onChange={(e) => setWorkspacePath(e.target.value)}
              placeholder="~/schmux-workspaces"
            />
            <p className="form-group__hint">
              Directory where cloned repositories will be stored. Only affects NEW workspaces.
            </p>
          </div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
        <div className="card__header">
          <h2 className="card__title">Repositories</h2>
        </div>
        <div className="card__body">
          {repos.length === 0 ? (
            <p className="text-muted">No repositories configured. Add at least one to start spawning sessions.</p>
          ) : (
            <div style={{ marginBottom: 'var(--spacing-md)' }}>
              {repos.map((repo) => (
                <div
                  key={repo.name}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    gap: 'var(--spacing-md)',
                    padding: 'var(--spacing-sm)',
                    backgroundColor: 'var(--color-bg-secondary)',
                    borderRadius: 'var(--border-radius)',
                    marginBottom: 'var(--spacing-xs)'
                  }}
                >
                  <div style={{ flex: 1, fontWeight: 500 }}>{repo.name}</div>
                  <div className="text-muted" style={{ flex: 2, fontSize: 'var(--font-size-sm)' }}>{repo.url}</div>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => removeRepo(repo.name)}
                  >
                    Remove
                  </button>
                </div>
              ))}
            </div>
          )}

          <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'flex-end' }}>
            <div className="form-group" style={{ flex: 1, marginBottom: 0 }}>
              <input
                type="text"
                className="input"
                placeholder="Name"
                value={newRepoName}
                onChange={(e) => setNewRepoName(e.target.value)}
              />
            </div>
            <div className="form-group" style={{ flex: 2, marginBottom: 0 }}>
              <input
                type="text"
                className="input"
                placeholder="git@github.com:user/repo.git"
                value={newRepoUrl}
                onChange={(e) => setNewRepoUrl(e.target.value)}
              />
            </div>
            <button type="button" className="btn btn--sm" onClick={addRepo}>Add</button>
          </div>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 'var(--spacing-lg)' }}>
        <div className="card__header">
          <h2 className="card__title">Agents</h2>
        </div>
        <div className="card__body">
          {agents.length === 0 ? (
            <p className="text-muted">No agents configured. Add at least one to start spawning sessions.</p>
          ) : (
            <div style={{ marginBottom: 'var(--spacing-md)' }}>
              {agents.map((agent) => (
                <div
                  key={agent.name}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    gap: 'var(--spacing-md)',
                    padding: 'var(--spacing-sm)',
                    backgroundColor: 'var(--color-bg-secondary)',
                    borderRadius: 'var(--border-radius)',
                    marginBottom: 'var(--spacing-xs)'
                  }}
                >
                  <div style={{ flex: 1, fontWeight: 500 }}>{agent.name}</div>
                  <div className="text-muted" style={{ flex: 2, fontSize: 'var(--font-size-sm)' }}>{agent.command}</div>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => removeAgent(agent.name)}
                  >
                    Remove
                  </button>
                </div>
              ))}
            </div>
          )}

          <div style={{ display: 'flex', gap: 'var(--spacing-sm)', alignItems: 'flex-end' }}>
            <div className="form-group" style={{ flex: 1, marginBottom: 0 }}>
              <input
                type="text"
                className="input"
                placeholder="Name"
                value={newAgentName}
                onChange={(e) => setNewAgentName(e.target.value)}
              />
            </div>
            <div className="form-group" style={{ flex: 2, marginBottom: 0 }}>
              <input
                type="text"
                className="input"
                placeholder="Command (e.g., claude, codex)"
                value={newAgentCommand}
                onChange={(e) => setNewAgentCommand(e.target.value)}
              />
            </div>
            <button type="button" className="btn btn--sm" onClick={addAgent}>Add</button>
          </div>
        </div>
      </div>

      <div className="form-group" style={{ marginTop: 'var(--spacing-lg)' }}>
        <button
          className="btn btn--primary"
          onClick={handleSave}
          disabled={saving}
        >
          {saving ? 'Saving...' : 'Save Configuration'}
        </button>
        {!warning && (
          <p className="form-group__hint" style={{ marginTop: 'var(--spacing-sm)' }}>
            Changes take effect immediately after saving.
          </p>
        )}
      </div>
    </>
  );
}
