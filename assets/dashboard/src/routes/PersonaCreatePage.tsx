import { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { createPersona, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import PersonaForm, { slugify } from '../components/PersonaForm';
import type { PersonaFormData } from '../components/PersonaForm';
import type { PersonaCreateRequest } from '../lib/types.generated';

export default function PersonaCreatePage() {
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const { success: toastSuccess, error: toastError } = useToast();

  const handleSave = async (formData: PersonaFormData) => {
    if (!formData.name || !formData.icon || !formData.color || !formData.prompt) {
      toastError('Name, icon, color, and prompt are required');
      return;
    }

    const id = formData.id || slugify(formData.name);
    if (!id) {
      toastError('A valid ID could not be generated from the name');
      return;
    }
    if (id === 'create') {
      toastError('"create" is a reserved ID — please choose a different name or ID');
      return;
    }

    setSaving(true);
    try {
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
      navigate('/personas');
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to save persona'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="page-content">
      <div className="page-header">
        <h1>Create Persona</h1>
        <Link to="/personas" className="btn">
          Back to Personas
        </Link>
      </div>

      <PersonaForm
        mode="create"
        saving={saving}
        onSave={handleSave}
        onCancel={() => navigate('/personas')}
      />
    </div>
  );
}
