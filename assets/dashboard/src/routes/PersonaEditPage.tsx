import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { getPersonas, updatePersona, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import PersonaForm from '../components/PersonaForm';
import type { PersonaFormData } from '../components/PersonaForm';
import type { Persona } from '../lib/types.generated';

export default function PersonaEditPage() {
  const { personaId } = useParams<{ personaId: string }>();
  const navigate = useNavigate();
  const [persona, setPersona] = useState<Persona | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const { success: toastSuccess, error: toastError } = useToast();

  const loadPersona = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getPersonas();
      const found = (data.personas || []).find((p) => p.id === personaId);
      if (found) {
        setPersona(found);
      } else {
        setError(`Persona "${personaId}" not found`);
      }
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load personas'));
    } finally {
      setLoading(false);
    }
  }, [personaId]);

  useEffect(() => {
    loadPersona();
  }, [loadPersona]);

  const handleSave = async (formData: PersonaFormData) => {
    if (!persona) return;
    if (!formData.name || !formData.icon || !formData.color || !formData.prompt) {
      toastError('Name, icon, color, and prompt are required');
      return;
    }

    setSaving(true);
    try {
      await updatePersona(persona.id, {
        name: formData.name,
        icon: formData.icon,
        color: formData.color,
        prompt: formData.prompt,
        expectations: formData.expectations,
      });
      toastSuccess(`Updated "${formData.name}"`);
      navigate('/personas');
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to save persona'));
    } finally {
      setSaving(false);
    }
  };

  if (loading || !persona) {
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

  const initialData: PersonaFormData = {
    id: persona.id,
    name: persona.name,
    icon: persona.icon,
    color: persona.color,
    prompt: persona.prompt,
    expectations: persona.expectations,
  };

  return (
    <div className="page-content">
      <div className="page-header">
        <h1>{persona.name}</h1>
        <Link to="/personas" className="btn">
          Back to Personas
        </Link>
      </div>

      <PersonaForm
        mode="edit"
        initialData={initialData}
        saving={saving}
        onSave={handleSave}
        onCancel={() => navigate('/personas')}
      />
    </div>
  );
}
