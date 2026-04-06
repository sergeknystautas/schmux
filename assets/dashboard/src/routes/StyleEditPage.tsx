import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { getStyles, updateStyle, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import StyleForm from '../components/StyleForm';
import type { StyleFormData } from '../components/StyleForm';
import type { Style } from '../lib/types.generated';

export default function StyleEditPage() {
  const { styleId } = useParams<{ styleId: string }>();
  const navigate = useNavigate();
  const [style, setStyle] = useState<Style | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert } = useModal();

  const loadStyle = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getStyles();
      const found = (data.styles || []).find((s) => s.id === styleId);
      if (found) {
        setStyle(found);
      } else {
        setError(`Style "${styleId}" not found`);
      }
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load styles'));
    } finally {
      setLoading(false);
    }
  }, [styleId]);

  useEffect(() => {
    loadStyle();
  }, [loadStyle]);

  const handleSave = async (formData: StyleFormData) => {
    if (!style) return;
    if (!formData.name || !formData.icon || !formData.prompt) {
      toastError('Name, icon, and prompt are required');
      return;
    }

    setSaving(true);
    try {
      await updateStyle(style.id, {
        name: formData.name,
        icon: formData.icon,
        tagline: formData.tagline,
        prompt: formData.prompt,
      });
      toastSuccess(`Updated "${formData.name}"`);
      navigate('/styles');
    } catch (err) {
      alert('Save Failed', getErrorMessage(err, 'Failed to save style'));
    } finally {
      setSaving(false);
    }
  };

  if (loading || !style) {
    return (
      <div className="page-content">
        <div className="page-header">
          <h1>Edit Style</h1>
        </div>
        {error ? (
          <>
            <p className="text-danger">{error}</p>
            <Link to="/styles" className="btn">
              Back to Styles
            </Link>
          </>
        ) : (
          <p className="text-muted">Loading...</p>
        )}
      </div>
    );
  }

  const initialData: StyleFormData = {
    id: style.id,
    name: style.name,
    icon: style.icon,
    tagline: style.tagline,
    prompt: style.prompt,
  };

  return (
    <div className="page-content">
      <div className="page-header">
        <h1>{style.name}</h1>
        <Link to="/styles" className="btn">
          Back to Styles
        </Link>
      </div>

      <StyleForm
        mode="edit"
        initialData={initialData}
        saving={saving}
        onSave={handleSave}
        onCancel={() => navigate('/styles')}
      />
    </div>
  );
}
