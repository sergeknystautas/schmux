import { useState, useEffect } from 'react';
import {
  getRemoteProfiles,
  createRemoteProfile,
  updateRemoteProfile,
  deleteRemoteProfile,
  getErrorMessage,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import type {
  RemoteProfile,
  RemoteProfileCreateRequest,
  RemoteProfileFlavor,
  RemoteVCSCommands,
} from '../lib/types';

interface ProfileFormData {
  display_name: string;
  host_type: string;
  workspace_path: string;
  vcs: string;
  connect_command: string;
  reconnect_command: string;
  provision_command: string;
  hostname_regex: string;
  vscode_command_template: string;
  flavors: RemoteProfileFlavor[];
  repo_base_path: string;
  workspace_path_template: string;
  remote_vcs_commands: RemoteVCSCommands;
}

const emptyForm: ProfileFormData = {
  display_name: '',
  host_type: '',
  workspace_path: '',
  vcs: 'git',
  connect_command: '',
  reconnect_command: '',
  provision_command: '',
  hostname_regex: '',
  vscode_command_template: '',
  flavors: [{ flavor: '' }],
  repo_base_path: '',
  workspace_path_template: '',
  remote_vcs_commands: {},
};

export default function RemoteSettingsPage() {
  const [profiles, setProfiles] = useState<RemoteProfile[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showModal, setShowModal] = useState(false);
  const [editingProfile, setEditingProfile] = useState<RemoteProfile | null>(null);
  const [formData, setFormData] = useState<ProfileFormData>(emptyForm);
  const [saving, setSaving] = useState(false);
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert, confirm: modalConfirm } = useModal();

  const loadProfiles = async () => {
    try {
      setLoading(true);
      const data = await getRemoteProfiles();
      setProfiles(data);
      setError('');
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load remote profiles'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadProfiles();
  }, []);

  const handleAdd = () => {
    setEditingProfile(null);
    setFormData(emptyForm);
    setShowModal(true);
  };

  const profileToFormData = (profile: RemoteProfile, forClone = false): ProfileFormData => ({
    display_name: forClone ? `${profile.display_name} (copy)` : profile.display_name,
    host_type: profile.host_type || '',
    workspace_path: profile.workspace_path || '',
    vcs: profile.vcs,
    connect_command: profile.connect_command || '',
    reconnect_command: profile.reconnect_command || '',
    provision_command: profile.provision_command || '',
    hostname_regex: profile.hostname_regex || '',
    vscode_command_template: profile.vscode_command_template || '',
    flavors: forClone
      ? profile.flavors.map((f) => ({ ...f, flavor: '' }))
      : profile.flavors.length > 0
        ? [...profile.flavors]
        : [{ flavor: '' }],
    repo_base_path: profile.repo_base_path || '',
    workspace_path_template: profile.workspace_path_template || '',
    remote_vcs_commands: profile.remote_vcs_commands || {},
  });

  const handleClone = (profile: RemoteProfile) => {
    setEditingProfile(null);
    setFormData(profileToFormData(profile, true));
    setShowModal(true);
  };

  const handleEdit = (profile: RemoteProfile) => {
    setEditingProfile(profile);
    setFormData(profileToFormData(profile));
    setShowModal(true);
  };

  const handleDelete = async (profile: RemoteProfile) => {
    if (
      !(await modalConfirm(`Delete "${profile.display_name}"? This cannot be undone.`, {
        danger: true,
      }))
    ) {
      return;
    }
    try {
      await deleteRemoteProfile(profile.id);
      toastSuccess(`Deleted "${profile.display_name}"`);
      loadProfiles();
    } catch (err) {
      alert('Delete Failed', getErrorMessage(err, 'Failed to delete profile'));
    }
  };

  const isPersistent = formData.host_type === 'persistent';

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.display_name.trim()) {
      toastError('Display name is required');
      return;
    }

    if (isPersistent) {
      if (!formData.repo_base_path.trim()) {
        toastError('Repo base path is required for persistent hosts');
        return;
      }
      if (!formData.workspace_path_template.trim()) {
        toastError('Workspace path template is required for persistent hosts');
        return;
      }
    } else {
      if (!formData.workspace_path.trim()) {
        toastError('Workspace path is required');
        return;
      }
      const validFlavors = formData.flavors.filter((f) => f.flavor.trim());
      if (validFlavors.length === 0) {
        toastError('At least one flavor is required');
        return;
      }
    }

    const validFlavors = formData.flavors.filter((f) => f.flavor.trim());
    const vcsCommands = formData.remote_vcs_commands;
    const hasVcsCommands =
      vcsCommands.create_worktree || vcsCommands.remove_worktree || vcsCommands.check_dirty;

    const request: RemoteProfileCreateRequest = {
      display_name: formData.display_name.trim(),
      vcs: formData.vcs,
      connect_command: formData.connect_command.trim() || undefined,
      reconnect_command: formData.reconnect_command.trim() || undefined,
      provision_command: formData.provision_command.trim() || undefined,
      hostname_regex: formData.hostname_regex.trim() || undefined,
      vscode_command_template: formData.vscode_command_template.trim() || undefined,
      host_type: formData.host_type || undefined,
      workspace_path: isPersistent ? undefined : formData.workspace_path.trim(),
      flavors: isPersistent
        ? undefined
        : validFlavors.map((f) => ({
            flavor: f.flavor.trim(),
            display_name: f.display_name?.trim() || undefined,
            workspace_path: f.workspace_path?.trim() || undefined,
            provision_command: f.provision_command?.trim() || undefined,
          })),
      repo_base_path: isPersistent ? formData.repo_base_path.trim() : undefined,
      workspace_path_template: isPersistent ? formData.workspace_path_template.trim() : undefined,
      remote_vcs_commands:
        isPersistent && hasVcsCommands
          ? {
              create_worktree: vcsCommands.create_worktree?.trim() || undefined,
              remove_worktree: vcsCommands.remove_worktree?.trim() || undefined,
              check_dirty: vcsCommands.check_dirty?.trim() || undefined,
            }
          : undefined,
    };

    try {
      setSaving(true);
      if (editingProfile) {
        await updateRemoteProfile(editingProfile.id, request);
        toastSuccess(`Updated "${formData.display_name}"`);
      } else {
        await createRemoteProfile(request);
        toastSuccess(`Created "${formData.display_name}"`);
      }
      setShowModal(false);
      loadProfiles();
    } catch (err) {
      alert('Save Failed', getErrorMessage(err, 'Failed to save profile'));
    } finally {
      setSaving(false);
    }
  };

  const addFlavor = () => {
    setFormData({
      ...formData,
      flavors: [...formData.flavors, { flavor: '' }],
    });
  };

  const removeFlavor = (index: number) => {
    setFormData({
      ...formData,
      flavors: formData.flavors.filter((_, i) => i !== index),
    });
  };

  const updateFlavor = (index: number, field: keyof RemoteProfileFlavor, value: string) => {
    const updated = [...formData.flavors];
    updated[index] = { ...updated[index], [field]: value };
    setFormData({ ...formData, flavors: updated });
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
        <button className="btn btn--primary" onClick={loadProfiles}>
          Retry
        </button>
      </div>
    );
  }

  return (
    <>
      <div className="wizard-step-content">
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            marginBottom: 'var(--spacing-md)',
          }}
        >
          <p className="m-0 text-muted">
            Configure remote host profiles for running agents on remote machines via SSH or custom
            connection tools.
          </p>
          <button className="btn btn--primary" style={{ flexShrink: 0 }} onClick={handleAdd}>
            + Add Profile
          </button>
        </div>

        {profiles.length === 0 ? (
          <div className="empty-state">
            <div className="empty-state__icon">+</div>
            <h3 className="empty-state__title">No Remote Profiles</h3>
            <p className="empty-state__description">
              Add a remote profile to enable spawning agents on remote hosts.
            </p>
            <button className="btn btn--primary" onClick={handleAdd}>
              Add Your First Profile
            </button>
          </div>
        ) : (
          <div className="flex-col gap-md">
            {profiles.map((profile) => (
              <div key={profile.id} className="card p-md">
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'flex-start',
                  }}
                >
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 'var(--spacing-sm)',
                        marginBottom: 'var(--spacing-sm)',
                      }}
                    >
                      <h3 style={{ margin: 0 }}>{profile.display_name}</h3>
                      <span
                        style={{
                          fontSize: '0.7rem',
                          fontWeight: 600,
                          padding: '2px 8px',
                          borderRadius: '4px',
                          background:
                            profile.host_type === 'persistent'
                              ? 'color-mix(in srgb, var(--color-info) 15%, transparent)'
                              : 'var(--color-bg-secondary)',
                          color:
                            profile.host_type === 'persistent'
                              ? 'var(--color-info)'
                              : 'var(--color-text-muted)',
                          textTransform: 'uppercase',
                          letterSpacing: '0.05em',
                        }}
                      >
                        {profile.host_type === 'persistent' ? 'persistent' : 'ephemeral'}
                      </span>
                      <code style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                        {profile.vcs}
                      </code>
                    </div>

                    {/* Key info — what makes this profile unique */}
                    <div
                      style={{
                        fontSize: '0.875rem',
                        display: 'grid',
                        gridTemplateColumns: 'auto 1fr',
                        gap: '2px var(--spacing-md)',
                        marginBottom: 'var(--spacing-xs)',
                      }}
                    >
                      {profile.host_type !== 'persistent' && profile.flavors.length > 0 && (
                        <>
                          <span className="text-muted">Flavors</span>
                          <span>
                            {profile.flavors.map((f) => f.display_name || f.flavor).join(', ')}
                          </span>
                        </>
                      )}
                      {profile.host_type !== 'persistent' && profile.workspace_path && (
                        <>
                          <span className="text-muted">Workspace</span>
                          <code style={{ fontSize: '0.8rem' }}>{profile.workspace_path}</code>
                        </>
                      )}
                      {profile.repo_base_path && (
                        <>
                          <span className="text-muted">Repo Base</span>
                          <code style={{ fontSize: '0.8rem' }}>{profile.repo_base_path}</code>
                        </>
                      )}
                      {profile.workspace_path_template && (
                        <>
                          <span className="text-muted">Workspace Template</span>
                          <code style={{ fontSize: '0.8rem' }}>
                            {profile.workspace_path_template}
                          </code>
                        </>
                      )}
                      {profile.connect_command && (
                        <>
                          <span className="text-muted">Connect</span>
                          <code style={{ fontSize: '0.8rem' }}>{profile.connect_command}</code>
                        </>
                      )}
                    </div>

                    {/* Collapsible details — less important fields */}
                    {(profile.reconnect_command ||
                      profile.provision_command ||
                      profile.hostname_regex ||
                      profile.vscode_command_template ||
                      profile.remote_vcs_commands?.create_worktree) && (
                      <details style={{ fontSize: '0.8rem' }}>
                        <summary
                          className="text-muted"
                          style={{ cursor: 'pointer', userSelect: 'none' }}
                        >
                          More details
                        </summary>
                        <div
                          style={{
                            display: 'grid',
                            gridTemplateColumns: 'auto 1fr',
                            gap: '2px var(--spacing-md)',
                            marginTop: 'var(--spacing-xs)',
                            paddingLeft: 'var(--spacing-xs)',
                          }}
                        >
                          {profile.reconnect_command && (
                            <>
                              <span className="text-muted">Reconnect</span>
                              <code>{profile.reconnect_command}</code>
                            </>
                          )}
                          {profile.provision_command && (
                            <>
                              <span className="text-muted">Provision</span>
                              <code>{profile.provision_command}</code>
                            </>
                          )}
                          {profile.hostname_regex && (
                            <>
                              <span className="text-muted">Hostname Regex</span>
                              <code>{profile.hostname_regex}</code>
                            </>
                          )}
                          {profile.vscode_command_template && (
                            <>
                              <span className="text-muted">VS Code</span>
                              <code>{profile.vscode_command_template}</code>
                            </>
                          )}
                          {profile.remote_vcs_commands?.create_worktree && (
                            <>
                              <span className="text-muted">Create Worktree</span>
                              <code>{profile.remote_vcs_commands.create_worktree}</code>
                            </>
                          )}
                          {profile.remote_vcs_commands?.remove_worktree && (
                            <>
                              <span className="text-muted">Remove Worktree</span>
                              <code>{profile.remote_vcs_commands.remove_worktree}</code>
                            </>
                          )}
                          {profile.remote_vcs_commands?.check_dirty && (
                            <>
                              <span className="text-muted">Check Dirty</span>
                              <code>{profile.remote_vcs_commands.check_dirty}</code>
                            </>
                          )}
                        </div>
                      </details>
                    )}
                  </div>
                  <div className="flex-row gap-xs">
                    <button className="btn btn--sm" onClick={() => handleClone(profile)}>
                      Clone
                    </button>
                    <button className="btn btn--sm" onClick={() => handleEdit(profile)}>
                      Edit
                    </button>
                    <button
                      className="btn btn--sm btn--danger"
                      onClick={() => handleDelete(profile)}
                    >
                      Delete
                    </button>
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
              <h2 className="modal__title">
                {editingProfile ? 'Edit Remote Profile' : 'Add Remote Profile'}
              </h2>
              <button className="modal__close" onClick={() => setShowModal(false)}>
                x
              </button>
            </div>
            <form onSubmit={handleSubmit}>
              <div className="modal__body">
                {/* Row 1: Name, Host Type, VCS */}
                <div
                  className="gap-md mb-md"
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '1fr auto auto',
                  }}
                >
                  <div className="form-group">
                    <label className="form-group__label" htmlFor="display_name">
                      Display Name *
                    </label>
                    <input
                      type="text"
                      id="display_name"
                      className="input"
                      value={formData.display_name}
                      onChange={(e) => setFormData({ ...formData, display_name: e.target.value })}
                      placeholder="e.g., Dev Server"
                      required
                    />
                  </div>
                  <div className="form-group" style={{ minWidth: '130px' }}>
                    <label className="form-group__label" htmlFor="host_type">
                      Host Type
                    </label>
                    <select
                      id="host_type"
                      className="select"
                      value={formData.host_type}
                      onChange={(e) => setFormData({ ...formData, host_type: e.target.value })}
                    >
                      <option value="">Ephemeral</option>
                      <option value="persistent">Persistent</option>
                    </select>
                  </div>
                  <div className="form-group" style={{ minWidth: '120px' }}>
                    <label className="form-group__label" htmlFor="vcs">
                      VCS
                    </label>
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

                {/* Flavors section — ephemeral only */}
                {!isPersistent && (
                  <div className="form-group mb-md">
                    <label className="form-group__label">Flavors *</label>
                    <span className="form-group__hint mb-sm">
                      Each flavor represents a host type within this profile (e.g., different
                      machine sizes).
                    </span>
                    {formData.flavors.map((f, i) => (
                      <div
                        key={i}
                        className="form-row mb-sm"
                        style={{ flexWrap: 'nowrap', alignItems: 'end' }}
                      >
                        <div className="form-group" style={{ marginBottom: 0, minWidth: 0 }}>
                          {i === 0 && (
                            <label className="form-group__label" style={{ fontSize: '0.75rem' }}>
                              Flavor String *
                            </label>
                          )}
                          <input
                            type="text"
                            className="input"
                            value={f.flavor}
                            onChange={(e) => updateFlavor(i, 'flavor', e.target.value)}
                            placeholder="e.g., dev.example.com"
                            required
                          />
                        </div>
                        <div className="form-group" style={{ marginBottom: 0, minWidth: 0 }}>
                          {i === 0 && (
                            <label className="form-group__label" style={{ fontSize: '0.75rem' }}>
                              Display Name
                            </label>
                          )}
                          <input
                            type="text"
                            className="input"
                            value={f.display_name || ''}
                            onChange={(e) => updateFlavor(i, 'display_name', e.target.value)}
                            placeholder="Optional label"
                          />
                        </div>
                        <div className="form-group" style={{ marginBottom: 0, minWidth: 0 }}>
                          {i === 0 && (
                            <label className="form-group__label" style={{ fontSize: '0.75rem' }}>
                              Workspace Path
                            </label>
                          )}
                          <input
                            type="text"
                            className="input"
                            value={f.workspace_path || ''}
                            onChange={(e) => updateFlavor(i, 'workspace_path', e.target.value)}
                            placeholder={formData.workspace_path || 'e.g., ~/workspace'}
                          />
                        </div>
                        <button
                          type="button"
                          className="btn btn--ghost btn--danger"
                          onClick={() => removeFlavor(i)}
                          disabled={formData.flavors.length <= 1}
                          style={{
                            flex: 'none',
                            fontSize: '1.25rem',
                            padding: 0,
                            width: '36px',
                            height: '36px',
                            justifyContent: 'center',
                          }}
                          title="Remove flavor"
                        >
                          ×
                        </button>
                      </div>
                    ))}
                    <button type="button" className="btn btn--sm mt-sm" onClick={addFlavor}>
                      + Add Flavor
                    </button>
                  </div>
                )}

                {/* Workspace Path — ephemeral only */}
                {!isPersistent && (
                  <div className="form-group mb-md">
                    <label className="form-group__label" htmlFor="workspace_path">
                      Default Workspace Path *
                    </label>
                    <input
                      type="text"
                      id="workspace_path"
                      className="input"
                      value={formData.workspace_path}
                      onChange={(e) => setFormData({ ...formData, workspace_path: e.target.value })}
                      placeholder="e.g., ~/workspace"
                      required
                    />
                    <span className="form-group__hint">
                      Default directory on the remote host. Flavors can override this.
                    </span>
                  </div>
                )}

                {/* Persistent host fields */}
                {isPersistent && (
                  <>
                    <div className="form-group mb-md">
                      <label className="form-group__label" htmlFor="repo_base_path">
                        Repo Base Path *
                      </label>
                      <input
                        type="text"
                        id="repo_base_path"
                        className="input"
                        value={formData.repo_base_path}
                        onChange={(e) =>
                          setFormData({ ...formData, repo_base_path: e.target.value })
                        }
                        placeholder="e.g., /home/user/myproject"
                        required
                      />
                      <span className="form-group__hint">
                        Path to the source repo on the remote host. Used as cwd for worktree
                        creation.
                      </span>
                    </div>

                    <div className="form-group mb-md">
                      <label className="form-group__label" htmlFor="workspace_path_template">
                        Workspace Path Template *
                      </label>
                      <input
                        type="text"
                        id="workspace_path_template"
                        className="input"
                        value={formData.workspace_path_template}
                        onChange={(e) =>
                          setFormData({ ...formData, workspace_path_template: e.target.value })
                        }
                        placeholder={'e.g., /home/user/schmux-ws/{{.WorkspaceID}}'}
                        required
                      />
                      <span className="form-group__hint">
                        Go template for new worktree paths. Must contain{' '}
                        <code>{'{{.WorkspaceID}}'}</code>.
                      </span>
                    </div>

                    <div className="form-group mb-md">
                      <label className="form-group__label">
                        VCS Commands <span className="font-normal text-muted">(optional)</span>
                      </label>
                      <span className="form-group__hint mb-sm">
                        Custom commands for worktree management. Leave empty to use defaults for the
                        selected VCS.
                      </span>
                      <div className="flex-col gap-sm">
                        <div className="form-group" style={{ marginBottom: 0 }}>
                          <label className="form-group__label" style={{ fontSize: '0.75rem' }}>
                            Create Worktree
                          </label>
                          <input
                            type="text"
                            className="input"
                            value={formData.remote_vcs_commands.create_worktree || ''}
                            onChange={(e) =>
                              setFormData({
                                ...formData,
                                remote_vcs_commands: {
                                  ...formData.remote_vcs_commands,
                                  create_worktree: e.target.value,
                                },
                              })
                            }
                            placeholder={
                              formData.vcs === 'sapling'
                                ? 'sl clone {{.RepoBasePath}} {{.DestPath}}'
                                : 'git worktree add {{.DestPath}} -b schmux-{{.WorkspaceID}} origin/main'
                            }
                          />
                        </div>
                        <div className="form-group" style={{ marginBottom: 0 }}>
                          <label className="form-group__label" style={{ fontSize: '0.75rem' }}>
                            Remove Worktree
                          </label>
                          <input
                            type="text"
                            className="input"
                            value={formData.remote_vcs_commands.remove_worktree || ''}
                            onChange={(e) =>
                              setFormData({
                                ...formData,
                                remote_vcs_commands: {
                                  ...formData.remote_vcs_commands,
                                  remove_worktree: e.target.value,
                                },
                              })
                            }
                            placeholder={
                              formData.vcs === 'sapling'
                                ? 'rm -rf {{.WorkspacePath}}'
                                : 'git worktree remove --force {{.WorkspacePath}}'
                            }
                          />
                        </div>
                        <div className="form-group" style={{ marginBottom: 0 }}>
                          <label className="form-group__label" style={{ fontSize: '0.75rem' }}>
                            Check Dirty
                          </label>
                          <input
                            type="text"
                            className="input"
                            value={formData.remote_vcs_commands.check_dirty || ''}
                            onChange={(e) =>
                              setFormData({
                                ...formData,
                                remote_vcs_commands: {
                                  ...formData.remote_vcs_commands,
                                  check_dirty: e.target.value,
                                },
                              })
                            }
                            placeholder={
                              formData.vcs === 'sapling'
                                ? 'sl status --cwd {{.WorkspacePath}}'
                                : 'git -C {{.WorkspacePath}} status --porcelain'
                            }
                          />
                        </div>
                      </div>
                    </div>
                  </>
                )}

                {/* Connect Command */}
                <div className="form-group mb-md">
                  <label className="form-group__label" htmlFor="connect_command">
                    Connect Command <span className="font-normal text-muted">(optional)</span>
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
                    Use <code>{'{{.Flavor}}'}</code> as placeholder. Defaults to{' '}
                    <code>ssh {'{{.Flavor}}'}</code>. Tmux control mode flags appended
                    automatically.
                  </span>
                </div>

                {/* Hostname Regex */}
                <div className="form-group mb-md">
                  <label className="form-group__label" htmlFor="hostname_regex">
                    Hostname Regex <span className="font-normal text-muted">(optional)</span>
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
                    Regex to extract hostname from connection STDOUT. First capture group{' '}
                    <code>()</code> is the hostname. Defaults to{' '}
                    <code>{'Establish ControlMaster connection to (\\S+)'}</code>.
                  </span>
                </div>

                {/* Reconnect Command */}
                <div className="form-group mb-md">
                  <label className="form-group__label" htmlFor="reconnect_command">
                    Reconnect Command <span className="font-normal text-muted">(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="reconnect_command"
                    className="input"
                    value={formData.reconnect_command}
                    onChange={(e) =>
                      setFormData({ ...formData, reconnect_command: e.target.value })
                    }
                    placeholder="e.g., ssh {{.Hostname}}"
                  />
                  <span className="form-group__hint">
                    Use <code>{'{{.Hostname}}'}</code> as placeholder. Defaults to connect command.
                    Tmux control mode flags appended automatically.
                  </span>
                </div>

                {/* Provision Command */}
                <div className="form-group mb-md">
                  <label className="form-group__label" htmlFor="provision_command">
                    Provision Command <span className="font-normal text-muted">(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="provision_command"
                    className="input"
                    value={formData.provision_command}
                    onChange={(e) =>
                      setFormData({ ...formData, provision_command: e.target.value })
                    }
                    placeholder="e.g., git clone https://github.com/user/repo.git {{.WorkspacePath}}"
                  />
                  <span className="form-group__hint">
                    Runs once after first connection. Use <code>{'{{.WorkspacePath}}'}</code> and{' '}
                    <code>{'{{.VCS}}'}</code> as placeholders.
                  </span>
                </div>

                {/* VS Code Template */}
                <div className="form-group">
                  <label className="form-group__label" htmlFor="vscode_command_template">
                    VS Code Template <span className="font-normal text-muted">(optional)</span>
                  </label>
                  <input
                    type="text"
                    id="vscode_command_template"
                    className="input"
                    value={formData.vscode_command_template}
                    onChange={(e) =>
                      setFormData({ ...formData, vscode_command_template: e.target.value })
                    }
                    placeholder="e.g., {{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
                  />
                  <span className="form-group__hint">
                    Use <code>{'{{.VSCodePath}}'}</code>, <code>{'{{.Hostname}}'}</code>,{' '}
                    <code>{'{{.Path}}'}</code>. Defaults to{' '}
                    <code>{'{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}'}</code>.
                  </span>
                </div>
              </div>
              <div className="modal__footer">
                <button
                  type="button"
                  className="btn"
                  onClick={() => setShowModal(false)}
                  disabled={saving}
                >
                  Cancel
                </button>
                <button type="submit" className="btn btn--primary" disabled={saving}>
                  {saving ? 'Saving...' : editingProfile ? 'Save Changes' : 'Add Profile'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  );
}
