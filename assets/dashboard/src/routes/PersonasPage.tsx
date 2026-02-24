import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import {
  getPersonas,
  createPersona,
  updatePersona,
  deletePersona,
  getErrorMessage,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import type { Persona, PersonaCreateRequest } from '../lib/types.generated';

interface PersonaFormData {
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

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

export default function PersonasPage() {
  const { personaId } = useParams<{ personaId?: string }>();
  const navigate = useNavigate();
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editingPersona, setEditingPersona] = useState<Persona | null>(null);
  const [formData, setFormData] = useState<PersonaFormData>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [autoSlug, setAutoSlug] = useState(true);
  const { success: toastSuccess, error: toastError } = useToast();
  const isCreateRoute = personaId === 'new';
  const isEditRoute = !!personaId && !isCreateRoute;

  const loadPersonas = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getPersonas();
      setPersonas(data.personas || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load personas'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadPersonas();
  }, [loadPersonas]);

  // When navigating to /personas/:personaId, load that persona into the edit form
  useEffect(() => {
    if (personaId && personaId !== 'new' && personas.length > 0) {
      const persona = personas.find((p) => p.id === personaId);
      if (persona) {
        setEditingPersona(persona);
        setFormData({
          id: persona.id,
          name: persona.name,
          icon: persona.icon,
          color: persona.color,
          prompt: persona.prompt,
          expectations: persona.expectations,
        });
        setAutoSlug(false);
      } else {
        setError(`Persona "${personaId}" not found`);
      }
    }
  }, [personaId, personas]);

  const handleCreate = () => {
    navigate('/personas/new');
  };

  const handleEdit = (persona: Persona) => {
    navigate(`/personas/${persona.id}`);
  };

  const handleDelete = async (persona: Persona) => {
    if (!confirm(`Are you sure you want to delete "${persona.name}"?`)) return;
    try {
      await deletePersona(persona.id);
      toastSuccess(`Deleted "${persona.name}"`);
      loadPersonas();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to delete persona'));
    }
  };

  const handleSave = async () => {
    if (!formData.name || !formData.icon || !formData.color || !formData.prompt) {
      toastError('Name, icon, color, and prompt are required');
      return;
    }
    setSaving(true);
    try {
      if (editingPersona) {
        await updatePersona(editingPersona.id, {
          name: formData.name,
          icon: formData.icon,
          color: formData.color,
          prompt: formData.prompt,
          expectations: formData.expectations,
        });
        toastSuccess(`Updated "${formData.name}"`);
      } else {
        const id = formData.id || slugify(formData.name);
        if (!id) {
          toastError('A valid ID could not be generated from the name');
          setSaving(false);
          return;
        }
        if (id === 'new') {
          toastError('"new" is a reserved ID — please choose a different name or ID');
          setSaving(false);
          return;
        }
        const req: PersonaCreateRequest = {
          id,
          name: formData.name,
          icon: formData.icon,
          color: formData.color,
          prompt: formData.prompt,
          expectations: formData.expectations,
        };
        await createPersona(req);
        toastSuccess(`Created "${formData.name}"`);
      }
      navigate('/personas');
      loadPersonas();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to save persona'));
    } finally {
      setSaving(false);
    }
  };

  const handleNameChange = (name: string) => {
    setFormData((prev) => ({
      ...prev,
      name,
      ...(autoSlug && !editingPersona ? { id: slugify(name) } : {}),
    }));
  };

  /** Shared form fields used in both create and edit views */
  const renderFormFields = () => (
    <>
      {/* Name + Icon + Color on one row */}
      <div className="form-row">
        <div className="form-group" style={{ flex: 1 }}>
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
    </>
  );

  // Create-route view: /personas/new
  if (isCreateRoute) {
    return (
      <div className="page-content">
        <div className="page-header">
          <h1>Create Persona</h1>
          <Link to="/personas" className="btn">
            Back to Personas
          </Link>
        </div>

        <div className="persona-form" data-testid="persona-form">
          {renderFormFields()}

          {/* ID slug row — only for create */}
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

          <div className="form-actions">
            <button className="btn" onClick={() => navigate('/personas')}>
              Cancel
            </button>
            <button className="btn btn--primary" onClick={handleSave} disabled={saving}>
              {saving ? 'Saving...' : 'Create'}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Edit-route view: /personas/:personaId
  if (isEditRoute) {
    if (loading || !editingPersona) {
      return (
        <div className="page-content">
          <div className="page-header">
            <h1>Edit Persona</h1>
          </div>
          {error ? (
            <>
              <p className="text-danger">{error}</p>
              <Link to="/personas" className="btn">
                Back to Personas
              </Link>
            </>
          ) : (
            <p className="text-muted">Loading...</p>
          )}
        </div>
      );
    }

    return (
      <div className="page-content">
        <div className="page-header">
          <h1>{editingPersona.name}</h1>
          <Link to="/personas" className="btn">
            Back to Personas
          </Link>
        </div>

        <div className="persona-form" data-testid="persona-form">
          {renderFormFields()}

          <div className="form-actions">
            <button className="btn" onClick={() => navigate('/personas')}>
              Cancel
            </button>
            <button className="btn btn--primary" onClick={handleSave} disabled={saving}>
              {saving ? 'Saving...' : 'Save Changes'}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="page-content">
        <div className="page-header">
          <h1>Personas</h1>
        </div>
        <p className="text-muted">Loading...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="page-content">
        <div className="page-header">
          <h1>Personas</h1>
        </div>
        <p className="text-danger">{error}</p>
      </div>
    );
  }

  // List view
  return (
    <div className="page-content">
      <div className="page-header">
        <h1>Personas</h1>
        <button className="btn btn--primary" onClick={handleCreate}>
          Create Persona
        </button>
      </div>

      <div className="persona-grid" data-testid="persona-grid">
        {personas.map((persona) => (
          <div key={persona.id} className="persona-card" data-testid={`persona-card-${persona.id}`}>
            <div className="persona-card__accent" style={{ backgroundColor: persona.color }} />
            <button
              className="persona-card__close"
              onClick={() => handleDelete(persona)}
              aria-label={`Delete ${persona.name}`}
              title="Delete"
            >
              &times;
            </button>
            <div className="persona-card__content">
              <div className="persona-card__header">
                <span className="persona-card__icon">{persona.icon}</span>
                <span className="persona-card__name">{persona.name}</span>
              </div>
              <p className="persona-card__preview">
                {persona.prompt.split('\n').slice(0, 2).join(' ').slice(0, 120)}
                {persona.prompt.length > 120 ? '...' : ''}
              </p>
              <div className="persona-card__actions">
                <button className="btn btn--sm btn--primary" onClick={() => handleEdit(persona)}>
                  Edit
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {personas.length === 0 && (
        <p className="text-muted">No personas yet. Create one to get started.</p>
      )}
    </div>
  );
}
