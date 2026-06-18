import { useState, useEffect, useCallback } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { getPersonas, deletePersona, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import type { Persona } from '../lib/types.generated';

export default function PersonasListPage() {
  const navigate = useNavigate();
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert, confirm: modalConfirm } = useModal();

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
    if (
      !(await modalConfirm(`Are you sure you want to delete "${persona.name}"?`, { danger: true }))
    )
      return;
    try {
      await deletePersona(persona.id);
      toastSuccess(`Deleted "${persona.name}"`);
      loadPersonas();
    } catch (err) {
      alert('Delete Failed', getErrorMessage(err, 'Failed to delete persona'));
    }
  };

  if (loading) {
    return (
      <div className="page-content">
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">Personas</h1>
          </div>
        </div>
        <p className="text-muted">Loading...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="page-content">
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">Personas</h1>
          </div>
        </div>
        <p className="text-danger">{error}</p>
      </div>
    );
  }

  return (
    <div className="page-content">
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Personas</h1>
        </div>
        <div className="app-header__actions">
          <Link to="/personas/create" className="btn btn--primary">
            Create Persona
          </Link>
        </div>
      </div>

      <div className="entity-grid" data-testid="persona-grid">
        {personas.map((persona) => (
          <div key={persona.id} className="entity-card" data-testid={`persona-card-${persona.id}`}>
            <div className="entity-card__accent" style={{ backgroundColor: persona.color }} />
            <button
              className="entity-card__close"
              onClick={() => handleDelete(persona)}
              aria-label={`Delete ${persona.name}`}
              title="Delete"
            >
              &times;
            </button>
            <div className="entity-card__content">
              <div className="entity-card__header">
                <span className="entity-card__icon">{persona.icon}</span>
                <span className="entity-card__name">{persona.name}</span>
              </div>
              <p className="entity-card__preview">
                {persona.prompt.split('\n').slice(0, 2).join(' ').slice(0, 120)}
                {persona.prompt.length > 120 ? '...' : ''}
              </p>
              <div className="entity-card__actions">
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
        <div className="empty-state">
          <h3 className="empty-state__title">No personas yet</h3>
          <p className="empty-state__description">Create one to get started.</p>
        </div>
      )}
    </div>
  );
}
