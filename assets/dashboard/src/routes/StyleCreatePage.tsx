import { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { createStyle, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import StyleForm, { slugify } from '../components/StyleForm';
import type { StyleFormData } from '../components/StyleForm';
import type { StyleCreateRequest } from '../lib/types.generated';

export default function StyleCreatePage() {
  const navigate = useNavigate();
  const [saving, setSaving] = useState(false);
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert } = useModal();

  const handleSave = async (formData: StyleFormData) => {
    if (!formData.name || !formData.icon || !formData.prompt) {
      toastError('Name, icon, and prompt are required');
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
      const req: StyleCreateRequest = {
        id,
        name: formData.name,
        icon: formData.icon,
        tagline: formData.tagline,
        prompt: formData.prompt,
      };
      await createStyle(req);
      toastSuccess(`Created "${formData.name}"`);
      navigate('/styles');
    } catch (err) {
      alert('Save Failed', getErrorMessage(err, 'Failed to save style'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="page-content">
      <div className="page-header">
        <h1>Create Style</h1>
        <Link to="/styles" className="btn">
          Back to Styles
        </Link>
      </div>

      <StyleForm
        mode="create"
        saving={saving}
        onSave={handleSave}
        onCancel={() => navigate('/styles')}
      />
    </div>
  );
}
