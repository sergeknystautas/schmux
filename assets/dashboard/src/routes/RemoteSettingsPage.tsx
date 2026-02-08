import { useState, useEffect } from 'react';
import { getRemoteFlavors, createRemoteFlavor, updateRemoteFlavor, deleteRemoteFlavor, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import type { RemoteFlavor, RemoteFlavorCreateRequest } from '../lib/types';

interface FlavorFormData {
  display_name: string;
  flavor: string;
  workspace_path: string;
  vcs: string;
  connect_command: string;
  reconnect_command: string;
  provision_command: string;
  hostname_regex: string;
  vscode_command_template: string;
}

const emptyForm: FlavorFormData = {
  display_name: '',
  flavor: '',
  workspace_path: '',
  vcs: 'sapling',
  connect_command: '',
  reconnect_command: '',
  provision_command: '',
  hostname_regex: '',
  vscode_command_template: '',
};

export default function RemoteSettingsPage() {
  const [flavors, setFlavors] = useState<RemoteFlavor[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showModal, setShowModal] = useState(false);
  const [editingFlavor, setEditingFlavor] = useState<RemoteFlavor | null>(null);
  const [formData, setFormData] = useState<FlavorFormData>(emptyForm);
  const [saving, setSaving] = useState(false);
  const { success: toastSuccess, error: toastError } = useToast();

  const loadFlavors = async () => {
    try {
      setLoading(true);
      const data = await getRemoteFlavors();
      setFlavors(data);
      setError('');
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load remote flavors'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadFlavors();
  }, []);

  const handleAdd = () => {
    setEditingFlavor(null);
    setFormData(emptyForm);
    setShowModal(true);
  };

  const handleEdit = (flavor: RemoteFlavor) => {
    setEditingFlavor(flavor);
    setFormData({
      display_name: flavor.display_name,
      flavor: flavor.flavor,
      workspace_path: flavor.workspace_path,
      vcs: flavor.vcs,
      connect_command: flavor.connect_command || '',
      reconnect_command: flavor.reconnect_command || '',
      provision_command: flavor.provision_command || '',
      hostname_regex: flavor.hostname_regex || '',
      vscode_command_template: flavor.vscode_command_template || '',
    });
    setShowModal(true);
  };

  const handleDelete = async (flavor: RemoteFlavor) => {
    if (!confirm(`Delete "${flavor.display_name}"? This cannot be undone.`)) {
      return;
    }
    try {
      await deleteRemoteFlavor(flavor.id);
      toastSuccess(`Deleted "${flavor.display_name}"`);
      loadFlavors();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to delete flavor'));
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.display_name.trim()) {
      toastError('Display name is required');
      return;
    }
    if (!formData.flavor.trim()) {
      toastError('Flavor string is required');
      return;
    }
    if (!formData.workspace_path.trim()) {
      toastError('Workspace path is required');
      return;
    }

    const request: RemoteFlavorCreateRequest = {
      display_name: formData.display_name.trim(),
      flavor: formData.flavor.trim(),
      workspace_path: formData.workspace_path.trim(),
      vcs: formData.vcs,
      connect_command: formData.connect_command.trim() || undefined,
      reconnect_command: formData.reconnect_command.trim() || undefined,
      provision_command: formData.provision_command.trim() || undefined,
      hostname_regex: formData.hostname_regex.trim() || undefined,
      vscode_command_template: formData.vscode_command_template.trim() || undefined,
    };

    try {
      setSaving(true);
      if (editingFlavor) {
        await updateRemoteFlavor(editingFlavor.id, request);
        toastSuccess(`Updated "${formData.display_name}"`);
      } else {
        await createRemoteFlavor(request);
        toastSuccess(`Created "${formData.display_name}"`);
      }
      setShowModal(false);
      loadFlavors();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to save flavor'));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading remote settings...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">!</div>
        <h3 className="empty-state__title">Error</h3>
        <p className="empty-state__description">{error}</p>
        <button className="btn btn--primary" onClick={loadFlavors}>Retry</button>
      </div>
    );
  }

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Remote Hosts</h1>
        </div>
        <div className="app-header__actions">
          <button className="btn btn--primary" onClick={handleAdd}>
            + Add Flavor
          </button>
        </div>
      </div>

      <div className="spawn-content">
        <p style={{ marginBottom: 'var(--spacing-lg)', color: 'var(--color-text-muted)' }}>
          Configure remote host flavors for running agents on remote machines via SSH or custom connection tools.
        </p>

        {flavors.length === 0 ? (
          <div className="empty-state">
            <div className="empty-state__icon">+</div>
            <h3 className="empty-state__title">No Remote Flavors</h3>
            <p className="empty-state__description">
              Add a remote flavor to enable spawning agents on remote hosts.
            </p>
            <button className="btn btn--primary" onClick={handleAdd}>
              Add Your First Flavor
            </button>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-md)' }}>
            {flavors.map((flavor) => (
              <div key={flavor.id} className="card" style={{ padding: 'var(--spacing-md)' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <div>
                    <h3 style={{ margin: 0, marginBottom: 'var(--spacing-xs)' }}>{flavor.display_name}</h3>
                    <div style={{ color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>
                      <div><strong>Flavor:</strong> <code>{flavor.flavor}</code></div>
                      <div><strong>Workspace:</strong> <code>{flavor.workspace_path}</code></div>
                      <div><strong>VCS:</strong> <code>{flavor.vcs}</code></div>
                      {flavor.connect_command && (
                        <div><strong>Connect:</strong> <code>{flavor.connect_command}</code></div>
                      )}
                      {flavor.reconnect_command && (
                        <div><strong>Reconnect:</strong> <code>{flavor.reconnect_command}</code></div>
                      )}
                      {flavor.provision_command && (
                        <div><strong>Provision:</strong> <code>{flavor.provision_command}</code></div>
                      )}
                      {flavor.hostname_regex && (
                        <div><strong>Hostname Regex:</strong> <code>{flavor.hostname_regex}</code></div>
                      )}
                      {flavor.vscode_command_template && (
                        <div><strong>VS Code:</strong> <code>{flavor.vscode_command_template}</code></div>
                      )}
                    </div>
                  </div>
                  <div style={{ display: 'flex', gap: 'var(--spacing-xs)' }}>
                    <button className="btn btn--sm" onClick={() => handleEdit(flavor)}>Edit</button>
                    <button className="btn btn--sm btn--danger" onClick={() => handleDelete(flavor)}>Delete</button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {showModal && (
        <div className="modal-overlay" onClick={() => setShowModal(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '900px' }}>
            <div className="modal__header">
              <h2 className="modal__title">{editingFlavor ? 'Edit Remote Flavor' : 'Add Remote Flavor'}</h2>
              <button className="modal__close" onClick={() => setShowModal(false)}>x</button>
            </div>
            <form onSubmit={handleSubmit}>
              <div className="modal__body">
                {/* Row 1: Name, Flavor, VCS side-by-side */}
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr auto', gap: 'var(--spacing-md)', marginBottom: 'var(--spacing-md)' }}>
                  <div className="form-group">
                    <label className="form-group__label" htmlFor="display_name">Display Name *</label>
                    <input
                      type="text"
                      id="display_name"
                      className="input"
                      value={formData.display_name}
                      onChange={(e) => setFormData({ ...formData, display_name: e.target.value })}
                      placeholder="e.g., GPU ML Large"
                      required
                    />
                  </div>
                  <div className="form-group">
                    <label className="form-group__label" htmlFor="flavor">Flavor String *</label>
                    <input
                      type="text"
                      id="flavor"
                      className="input"
                      value={formData.flavor}
                      onChange={(e) => setFormData({ ...formData, flavor: e.target.value })}
                      placeholder="e.g., dev.example.com"
                      required
                    />
                  </div>
                  <div className="form-group" style={{ minWidth: '120px' }}>
                    <label className="form-group__label" htmlFor="vcs">VCS</label>
                    <select
                      id="vcs"
                      className="select"
                      value={formData.vcs}
                      onChange={(e) => setFormData({ ...formData, vcs: e.target.value })}
                    >
                      <option value="sapling">Sapling</option>
                      <option value="git">Git</option>
                    </select>
                  </div>
                </div>

                {/* Row 2: Workspace Path full-width */}
                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label" htmlFor="workspace_path">Workspace Path *</label>
                  <input
                    type="text"
                    id="workspace_path"
                    className="input"
                    value={formData.workspace_path}
                    onChange={(e) => setFormData({ ...formData, workspace_path: e.target.value })}
                    placeholder="e.g., ~/fbsource"
                    required
                  />
                  <span className="form-group__hint">Directory where code lives on the remote host</span>
                </div>

                {/* Row 3: Connect Command */}
                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label" htmlFor="connect_command">
                    Connect Command <span style={{ fontWeight: 'normal', color: 'var(--color-text-muted)' }}>(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="connect_command"
                    className="input"
                    value={formData.connect_command}
                    onChange={(e) => setFormData({ ...formData, connect_command: e.target.value })}
                    placeholder="e.g., ssh {{.Flavor}}"
                  />
                  <span className="form-group__hint">
                    Use <code>{'{{.Flavor}}'}</code> as placeholder.
                    Defaults to <code>ssh {'{{.Flavor}}'}</code>. Tmux control mode flags appended automatically.
                  </span>
                </div>

                {/* Row 4: Hostname Regex (related to connect output) */}
                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label" htmlFor="hostname_regex">
                    Hostname Regex <span style={{ fontWeight: 'normal', color: 'var(--color-text-muted)' }}>(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="hostname_regex"
                    className="input"
                    value={formData.hostname_regex}
                    onChange={(e) => setFormData({ ...formData, hostname_regex: e.target.value })}
                    placeholder="e.g., Establish ControlMaster connection to (\S+)"
                  />
                  <span className="form-group__hint">
                    Regex to extract hostname from connection STDOUT. First capture group <code>()</code> is the hostname.
                    Defaults to <code>{'Establish ControlMaster connection to (\\S+)'}</code>.
                  </span>
                </div>

                {/* Row 5: Reconnect Command */}
                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label" htmlFor="reconnect_command">
                    Reconnect Command <span style={{ fontWeight: 'normal', color: 'var(--color-text-muted)' }}>(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="reconnect_command"
                    className="input"
                    value={formData.reconnect_command}
                    onChange={(e) => setFormData({ ...formData, reconnect_command: e.target.value })}
                    placeholder="e.g., ssh {{.Hostname}}"
                  />
                  <span className="form-group__hint">
                    Use <code>{'{{.Hostname}}'}</code> as placeholder.
                    Defaults to connect command. Tmux control mode flags appended automatically.
                  </span>
                </div>

                {/* Row 6: Provision Command */}
                <div className="form-group" style={{ marginBottom: 'var(--spacing-md)' }}>
                  <label className="form-group__label" htmlFor="provision_command">
                    Provision Command <span style={{ fontWeight: 'normal', color: 'var(--color-text-muted)' }}>(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="provision_command"
                    className="input"
                    value={formData.provision_command}
                    onChange={(e) => setFormData({ ...formData, provision_command: e.target.value })}
                    placeholder="e.g., git clone https://github.com/user/repo.git {{.WorkspacePath}}"
                  />
                  <span className="form-group__hint">
                    Runs once after first connection. Use <code>{'{{.WorkspacePath}}'}</code> and <code>{'{{.VCS}}'}</code> as placeholders.
                  </span>
                </div>

                {/* Row 7: VS Code Template */}
                <div className="form-group">
                  <label className="form-group__label" htmlFor="vscode_command_template">
                    VS Code Template <span style={{ fontWeight: 'normal', color: 'var(--color-text-muted)' }}>(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="vscode_command_template"
                    className="input"
                    value={formData.vscode_command_template}
                    onChange={(e) => setFormData({ ...formData, vscode_command_template: e.target.value })}
                    placeholder="e.g., {{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
                  />
                  <span className="form-group__hint">
                    Use <code>{'{{.VSCodePath}}'}</code>, <code>{'{{.Hostname}}'}</code>, <code>{'{{.Path}}'}</code>. Defaults to <code>{'{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}'}</code>.
                  </span>
                </div>
              </div>
              <div className="modal__footer">
                <button type="button" className="btn" onClick={() => setShowModal(false)} disabled={saving}>
                  Cancel
                </button>
                <button type="submit" className="btn btn--primary" disabled={saving}>
                  {saving ? 'Saving...' : editingFlavor ? 'Save Changes' : 'Add Flavor'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  );
}
