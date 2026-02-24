import { useState, useEffect, useCallback } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { getPersonas, deletePersona, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import type { Persona } from '../lib/types.generated';

export default function PersonasListPage() {
  const navigate = useNavigate();
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const { success: toastSuccess, error: toastError } = useToast();

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

  return (
    <div className="page-content">
      <div className="page-header">
        <h1>Personas</h1>
        <Link to="/personas/create" className="btn btn--primary">
          Create Persona
        </Link>
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
                <button
                  className="btn btn--sm btn--primary"
                  onClick={() => navigate(`/personas/${persona.id}`)}
                >
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
