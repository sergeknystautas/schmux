import { useState } from 'react';
import { createSpawnEntry } from '../lib/spawn-api';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import styles from './CreateActionForm.module.css';

type CreateActionFormProps = {
  repo: string;
  onCreated: () => void;
  onCancel: () => void;
};

export default function CreateActionForm({ repo, onCreated, onCancel }: CreateActionFormProps) {
  const { success } = useToast();
  const { alert } = useModal();
  const [name, setName] = useState('');
  const [type, setType] = useState<'command' | 'agent'>('command');
  const [command, setCommand] = useState('');
  const [prompt, setPrompt] = useState('');
  const [target, setTarget] = useState('');
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    setSaving(true);
    try {
      await createSpawnEntry(repo, {
        name: name.trim(),
        type,
        command: type === 'command' ? command : undefined,
        prompt: type === 'agent' ? prompt : undefined,
        target: type === 'agent' && target ? target : undefined,
      });
      success(`Created "${name.trim()}"`);
      onCreated();
    } catch (err) {
      alert('Save Failed', err instanceof Error ? err.message : 'Failed to create action');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="action-name">
          Name
        </label>
        <input
          id="action-name"
          className={styles.input}
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. Run tests"
          required
        />
      </div>

      <div className={styles.field}>
        <label className={styles.label}>Type</label>
        <div className={styles.radioGroup}>
          <label className={styles.radio}>
            <input
              type="radio"
              name="action-type"
              value="command"
              checked={type === 'command'}
              onChange={() => setType('command')}
            />
            Shell command
          </label>
          <label className={styles.radio}>
            <input
              type="radio"
              name="action-type"
              value="agent"
              checked={type === 'agent'}
              onChange={() => setType('agent')}
            />
            Agent session
          </label>
        </div>
      </div>

      {type === 'command' && (
        <div className={styles.field}>
          <label className={styles.label} htmlFor="action-command">
            Command
          </label>
          <input
            id="action-command"
            className={styles.input}
            type="text"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder="e.g. go test ./..."
          />
        </div>
      )}

      {type === 'agent' && (
        <>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="action-prompt">
              Prompt
            </label>
            <textarea
              id="action-prompt"
              className={styles.textarea}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="e.g. Fix lint errors in the project"
              rows={3}
            />
          </div>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="action-target">
              Target (optional)
            </label>
            <input
              id="action-target"
              className={styles.input}
              type="text"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder="e.g. claude-code"
            />
          </div>
        </>
      )}

      <div className={styles.buttons}>
        <button type="button" className={styles.cancelButton} onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className={styles.saveButton} disabled={saving || !name.trim()}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>
    </form>
  );
}
