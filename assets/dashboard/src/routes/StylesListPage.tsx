import { useState, useEffect, useCallback } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { getStyles, deleteStyle, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import type { Style } from '../lib/types.generated';

export default function StylesListPage() {
  const navigate = useNavigate();
  const [styles, setStyles] = useState<Style[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert, confirm: modalConfirm } = useModal();

  const loadStyles = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getStyles();
      setStyles(data.styles || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load styles'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadStyles();
  }, [loadStyles]);

  const handleDelete = async (style: Style) => {
    if (!(await modalConfirm(`Are you sure you want to delete "${style.name}"?`, { danger: true })))
      return;
    try {
      await deleteStyle(style.id);
      toastSuccess(`Deleted "${style.name}"`);
      loadStyles();
    } catch (err) {
      alert('Delete Failed', getErrorMessage(err, 'Failed to delete style'));
    }
  };

  if (loading) {
    return (
      <div className="page-content">
        <div className="app-header">
          <div className="app-header__info">
            <h1 className="app-header__meta">Comm Styles</h1>
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
            <h1 className="app-header__meta">Comm Styles</h1>
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
          <h1 className="app-header__meta">Comm Styles</h1>
        </div>
        <div className="app-header__actions">
          <Link to="/styles/create" className="btn btn--primary">
            Create Style
          </Link>
        </div>
      </div>

      <div className="entity-grid" data-testid="style-grid">
        {styles.map((style) => (
          <div key={style.id} className="entity-card" data-testid={`style-card-${style.id}`}>
            <button
              className="entity-card__close"
              onClick={() => handleDelete(style)}
              aria-label={`Delete ${style.name}`}
              title="Delete"
            >
              &times;
            </button>
            <div className="entity-card__content">
              <div className="entity-card__header">
                <span className="entity-card__icon">{style.icon}</span>
                <span className="entity-card__name">{style.name}</span>
              </div>
              <p className="entity-card__preview">{style.tagline || ''}</p>
              <div className="entity-card__actions">
                <button
                  className="btn btn--sm btn--primary"
                  onClick={() => navigate(`/styles/${style.id}`)}
                >
                  Edit
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {styles.length === 0 && (
        <div className="empty-state">
          <h3 className="empty-state__title">No styles yet</h3>
          <p className="empty-state__description">Create one to get started.</p>
        </div>
      )}
    </div>
  );
}
