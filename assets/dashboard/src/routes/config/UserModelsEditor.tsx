import { useState, useEffect } from 'react';

interface UserModel {
  id: string;
  display_name: string;
  provider: string;
  runners: string[];
  command: string;
  required_env?: string[];
}

type UserModelsEditorProps = {
  availableRunners: string[];
  onRefreshCatalog?: () => void;
};

export default function UserModelsEditor({
  availableRunners,
  onRefreshCatalog,
}: UserModelsEditorProps) {
  const [models, setModels] = useState<UserModel[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Form state
  const [editingId, setEditingId] = useState<string | null>(null);
  const [formData, setFormData] = useState({
    id: '',
    display_name: '',
    provider: 'custom',
    runners: [] as string[],
    command: '',
    required_env: '',
  });

  useEffect(() => {
    fetchUserModels();
  }, []);

  async function fetchUserModels() {
    try {
      const resp = await fetch('/api/user-models');
      if (!resp.ok) throw new Error('Failed to fetch user models');
      const data = await resp.json();
      setModels(data.models || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  }

  async function saveModels(newModels: UserModel[]) {
    setSaving(true);
    setError(null);
    try {
      const resp = await fetch('/api/user-models', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ models: newModels }),
      });
      if (!resp.ok) {
        const err = await resp.json();
        throw new Error(err.error || 'Failed to save user models');
      }
      setModels(newModels);
      if (onRefreshCatalog) onRefreshCatalog();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unknown error');
    } finally {
      setSaving(false);
    }
  }

  function handleAdd() {
    setEditingId('new');
    setFormData({
      id: '',
      display_name: '',
      provider: 'custom',
      runners: [],
      command: '',
      required_env: '',
    });
  }

  function handleEdit(model: UserModel) {
    setEditingId(model.id);
    setFormData({
      id: model.id,
      display_name: model.display_name,
      provider: model.provider,
      runners: model.runners,
      command: model.command,
      required_env: model.required_env?.join(', ') || '',
    });
  }

  function handleDelete(id: string) {
    if (!confirm('Delete this user model?')) return;
    saveModels(models.filter((m) => m.id !== id));
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const requiredEnv = formData.required_env
      ? formData.required_env
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean)
      : undefined;

    const newModel: UserModel = {
      id: formData.id || formData.display_name.toLowerCase().replace(/\s+/g, '-'),
      display_name: formData.display_name,
      provider: formData.provider,
      runners: formData.runners,
      command: formData.command,
      required_env: requiredEnv,
    };

    let newModels: UserModel[];
    if (editingId === 'new') {
      newModels = [...models, newModel];
    } else {
      newModels = models.map((m) => (m.id === editingId ? newModel : m));
    }

    saveModels(newModels);
    setEditingId(null);
  }

  function handleCancel() {
    setEditingId(null);
  }

  function toggleRunner(runner: string) {
    const current = formData.runners;
    if (current.includes(runner)) {
      setFormData({ ...formData, runners: current.filter((r) => r !== runner) });
    } else {
      setFormData({ ...formData, runners: [...current, runner] });
    }
  }

  if (loading) {
    return <div className="user-models-editor">Loading...</div>;
  }

  return (
    <div className="user-models-editor">
      <div className="user-models-editor__header">
        <h3>User-Defined Models</h3>
        <button className="btn btn--sm btn--primary" onClick={handleAdd}>
          Add Model
        </button>
      </div>

      {error && <div className="error-message">{error}</div>}

      {models.length === 0 && !editingId && (
        <p className="user-models-editor__empty">
          No user-defined models. Click &ldquo;Add Model&rdquo; to create one.
        </p>
      )}

      {models.length > 0 && (
        <div className="user-models-editor__list">
          {models.map((model) => (
            <div key={model.id} className="user-models-editor__model">
              <div className="user-models-editor__model-info">
                <strong>{model.display_name}</strong>
                <span className="user-models-editor__model-meta">
                  {model.provider} | {model.runners.join(', ')}
                </span>
              </div>
              <div className="user-models-editor__model-actions">
                <button className="btn btn--sm" onClick={() => handleEdit(model)}>
                  Edit
                </button>
                <button className="btn btn--sm btn--danger" onClick={() => handleDelete(model.id)}>
                  Delete
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {editingId && (
        <div className="user-models-editor__form">
          <h4>{editingId === 'new' ? 'Add User Model' : 'Edit User Model'}</h4>
          <form onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Display Name</label>
              <input
                className="input"
                type="text"
                value={formData.display_name}
                onChange={(e) => setFormData({ ...formData, display_name: e.target.value })}
                required
                placeholder="My Custom Model"
              />
            </div>

            <div className="form-group">
              <label>Provider</label>
              <input
                className="input"
                type="text"
                value={formData.provider}
                onChange={(e) => setFormData({ ...formData, provider: e.target.value })}
                placeholder="custom"
              />
            </div>

            <div className="form-group">
              <label>Command</label>
              <input
                className="input"
                type="text"
                value={formData.command}
                onChange={(e) => setFormData({ ...formData, command: e.target.value })}
                required
                placeholder="claude --dangerously-skip-permissions"
              />
            </div>

            <div className="form-group">
              <label>Runners (select available tools)</label>
              <div className="runner-checkboxes">
                {availableRunners.map((runner) => (
                  <label key={runner} className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={formData.runners.includes(runner)}
                      onChange={() => toggleRunner(runner)}
                    />
                    {runner}
                  </label>
                ))}
              </div>
            </div>

            <div className="form-group">
              <label>Required Environment Variables (comma-separated)</label>
              <input
                className="input"
                type="text"
                value={formData.required_env}
                onChange={(e) => setFormData({ ...formData, required_env: e.target.value })}
                placeholder="API_KEY, ENDPOINT_URL"
              />
            </div>

            <div className="form-actions">
              <button type="submit" className="btn btn--primary" disabled={saving}>
                {saving ? 'Saving...' : 'Save'}
              </button>
              <button type="button" className="btn" onClick={handleCancel}>
                Cancel
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
