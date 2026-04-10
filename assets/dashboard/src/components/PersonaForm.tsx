import { useState } from 'react';

export interface PersonaFormData {
  id: string;
  name: string;
  icon: string;
  color: string;
  prompt: string;
  expectations: string;
}

const emptyForm: PersonaFormData = {
  id: '',
  name: '',
  icon: '',
  color: '#3498db',
  prompt: '',
  expectations: '',
};

export function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

interface PersonaFormProps {
  mode: 'create' | 'edit';
  initialData?: PersonaFormData;
  saving: boolean;
  onSave: (data: PersonaFormData) => void;
  onCancel: () => void;
}

export default function PersonaForm({
  mode,
  initialData,
  saving,
  onSave,
  onCancel,
}: PersonaFormProps) {
  const [formData, setFormData] = useState<PersonaFormData>(initialData ?? emptyForm);
  const [autoSlug, setAutoSlug] = useState(mode === 'create');

  const handleNameChange = (name: string) => {
    setFormData((prev) => ({
      ...prev,
      name,
      ...(autoSlug && mode === 'create' ? { id: slugify(name) } : {}),
    }));
  };

  return (
    <div className="persona-form" data-testid="persona-form">
      {/* Name + Icon + Color on one row */}
      <div className="form-row">
        <div className="form-group flex-1">
          <label className="form-group__label" htmlFor="persona-name">
            Name
          </label>
          <input
            id="persona-name"
            type="text"
            className="input"
            value={formData.name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder="Security Auditor"
          />
        </div>
        <div className="form-group" style={{ flex: '0 0 auto', minWidth: 0 }}>
          <label className="form-group__label" htmlFor="persona-icon">
            Icon (emoji)
          </label>
          <input
            id="persona-icon"
            type="text"
            className="input"
            value={formData.icon}
            onChange={(e) => setFormData((prev) => ({ ...prev, icon: e.target.value }))}
            placeholder="🔒"
            style={{ width: '5rem', textAlign: 'center', fontSize: '1.2rem' }}
          />
        </div>
        <div className="form-group" style={{ flex: '0 0 auto', minWidth: 0 }}>
          <label className="form-group__label" htmlFor="persona-color">
            Color
          </label>
          <div className="persona-form__color-wrapper">
            <input
              id="persona-color"
              type="color"
              className="persona-form__color-input"
              value={formData.color}
              onChange={(e) => setFormData((prev) => ({ ...prev, color: e.target.value }))}
            />
            <span className="persona-form__color-value">{formData.color}</span>
          </div>
        </div>
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="persona-prompt">
          Personality
        </label>
        <span
          className="form-group__hint"
          style={{ marginTop: 0, marginBottom: '4px', display: 'block' }}
        >
          Define how this agent should behave — its role, approach, and style
        </span>
        <textarea
          id="persona-prompt"
          className="textarea"
          style={{ fontFamily: 'var(--font-mono)' }}
          value={formData.prompt}
          onChange={(e) => setFormData((prev) => ({ ...prev, prompt: e.target.value }))}
          rows={25}
          placeholder="You are a security expert. Analyze code for vulnerabilities..."
        />
      </div>

      <div className="form-group">
        <label className="form-group__label" htmlFor="persona-expectations">
          Expectations
        </label>
        <span
          className="form-group__hint"
          style={{ marginTop: 0, marginBottom: '4px', display: 'block' }}
        >
          Describe the output format or deliverables expected from this persona
        </span>
        <textarea
          id="persona-expectations"
          className="textarea"
          style={{ fontFamily: 'var(--font-mono)' }}
          value={formData.expectations}
          onChange={(e) => setFormData((prev) => ({ ...prev, expectations: e.target.value }))}
          rows={3}
          placeholder="Produce a structured security report with severity ratings..."
        />
      </div>

      {/* ID slug row — only for create */}
      {mode === 'create' && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-group__label" htmlFor="persona-id">
              ID (slug)
            </label>
            <input
              id="persona-id"
              type="text"
              className="input"
              value={formData.id}
              onChange={(e) => {
                setAutoSlug(false);
                setFormData((prev) => ({ ...prev, id: e.target.value }));
              }}
              placeholder="security-auditor"
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
