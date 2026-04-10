import { useState } from 'react';

export interface StyleFormData {
  id: string;
  name: string;
  icon: string;
  tagline: string;
  prompt: string;
}

const emptyForm: StyleFormData = {
  id: '',
  name: '',
  icon: '',
  tagline: '',
  prompt: '',
};

export function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

interface StyleFormProps {
  mode: 'create' | 'edit';
  initialData?: StyleFormData;
  saving: boolean;
  onSave: (data: StyleFormData) => void;
  onCancel: () => void;
}

export default function StyleForm({ mode, initialData, saving, onSave, onCancel }: StyleFormProps) {
  const [formData, setFormData] = useState<StyleFormData>(initialData ?? emptyForm);
  const [autoSlug, setAutoSlug] = useState(mode === 'create');

  const handleNameChange = (name: string) => {
    setFormData((prev) => ({
      ...prev,
      name,
      ...(autoSlug && mode === 'create' ? { id: slugify(name) } : {}),
    }));
  };

  return (
    <div className="persona-form" data-testid="style-form">
      {/* Name + Icon on one row */}
      <div className="form-row">
        <div className="form-group flex-1">
          <label className="form-group__label" htmlFor="style-name">
            Name
          </label>
          <input
            id="style-name"
            type="text"
            className="input"
            value={formData.name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder="Concise Engineer"
          />
        </div>
        <div className="form-group" style={{ flex: '0 0 auto', minWidth: 0 }}>
          <label className="form-group__label" htmlFor="style-icon">
            Icon (emoji)
          </label>
          <input
            id="style-icon"
            type="text"
            className="input"
            value={formData.icon}
            onChange={(e) => setFormData((prev) => ({ ...prev, icon: e.target.value }))}
            placeholder="🎯"
            style={{ width: '5rem', textAlign: 'center', fontSize: '1.2rem' }}
          />
        </div>
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="style-tagline">
          Tagline
        </label>
        <input
          id="style-tagline"
          type="text"
          className="input"
          value={formData.tagline}
          onChange={(e) => setFormData((prev) => ({ ...prev, tagline: e.target.value }))}
          placeholder="Short, punchy description of this communication style"
        />
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="style-prompt">
          Prompt
        </label>
        <span
          className="form-group__hint"
          style={{ marginTop: 0, marginBottom: '4px', display: 'block' }}
        >
          Define the communication style — tone, verbosity, formatting preferences
        </span>
        <textarea
          id="style-prompt"
          className="textarea"
          style={{ fontFamily: 'var(--font-mono)' }}
          value={formData.prompt}
          onChange={(e) => setFormData((prev) => ({ ...prev, prompt: e.target.value }))}
          rows={15}
          placeholder="Communicate in a concise, direct manner. Avoid filler words..."
        />
      </div>

      {/* ID slug row — only for create */}
      {mode === 'create' && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-group__label" htmlFor="style-id">
              ID (slug)
            </label>
            <input
              id="style-id"
              type="text"
              className="input"
              value={formData.id}
              onChange={(e) => {
                setAutoSlug(false);
                setFormData((prev) => ({ ...prev, id: e.target.value }));
              }}
              placeholder="concise-engineer"
            />
          </div>
        </div>
      )}

      <div className="form-actions">
        <button className="btn" onClick={onCancel}>
          Cancel
        </button>
        <button className="btn btn--primary" onClick={() => onSave(formData)} disabled={saving}>
          {saving ? 'Saving...' : mode === 'create' ? 'Create' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}
