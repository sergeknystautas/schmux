import { useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'react';
import styles from './chat.module.css';
import type { ChatImage } from '../../lib/chat/types';
import type { ChatDraft } from '../../lib/chat-draft';
import type { WorkspaceAttachment } from '../../lib/types.generated';
import { getErrorMessage, uploadWorkspaceAttachment } from '../../lib/api';

export interface ComposerHandle {
  focus(position?: number): void;
  insert(text: string): void;
}

interface ComposerProps {
  workspaceId?: string;
  disabled: boolean;
  disabledReason?: string;
  ended: boolean;
  onSend(text: string, images: ChatImage[]): void;
  /** Text and attachments to start with (a draft restored for this session). */
  initialDraft?: ChatDraft;
  /** Called with the current text and attachments whenever either changes. */
  onDraftChange?(draft: ChatDraft): void;
  /** Called with the caret position whenever focus or the caret moves. */
  onCaretChange?(position: number): void;
  ref?: React.Ref<ComposerHandle>;
}

function readFileAsImage(file: File): Promise<ChatImage> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      const base64 = url.slice(url.indexOf(',') + 1);
      resolve({ media_type: file.type || 'image/png', data: base64 });
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

export default function Composer({
  workspaceId,
  disabled,
  disabledReason,
  ended,
  onSend,
  initialDraft,
  onDraftChange,
  onCaretChange,
  ref,
}: ComposerProps) {
  const [value, setValue] = useState(initialDraft?.text ?? '');
  const [images, setImages] = useState<ChatImage[]>(initialDraft?.images ?? []);
  const [files, setFiles] = useState<WorkspaceAttachment[]>(initialDraft?.files ?? []);
  const [attaching, setAttaching] = useState(false);
  const [attachmentError, setAttachmentError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  // Report the draft on every change so the page can persist it per session.
  const onDraftChangeRef = useRef(onDraftChange);
  onDraftChangeRef.current = onDraftChange;
  // Report the caret so the page can persist where focus was last.
  const onCaretChangeRef = useRef(onCaretChange);
  onCaretChangeRef.current = onCaretChange;
  const reportCaret = (el: HTMLTextAreaElement) => onCaretChangeRef.current?.(el.selectionStart);
  const mountedRef = useRef(false);
  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true; // the initial draft came from storage; nothing to save yet
      return;
    }
    onDraftChangeRef.current?.({ text: value, images, ...(files.length ? { files } : {}) });
  }, [value, images, files]);

  // Grow with the content so the whole draft stays visible; the stylesheet
  // caps the height and scrolls past it. Measured from scrollHeight, so this
  // is a genuinely dynamic inline value.
  useLayoutEffect(() => {
    const ta = textareaRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    if (ta.scrollHeight > 0) ta.style.height = `${ta.scrollHeight}px`;
  }, [value]);

  useImperativeHandle(
    ref,
    () => ({
      focus: (position?: number) => {
        const ta = textareaRef.current;
        if (!ta) return;
        ta.focus();
        const pos = Math.min(position ?? ta.value.length, ta.value.length);
        ta.selectionStart = ta.selectionEnd = pos;
      },
      insert: (text: string) => {
        const ta = textareaRef.current;
        if (!ta) return;
        const start = ta.selectionStart ?? value.length;
        const end = ta.selectionEnd ?? value.length;
        const next = value.slice(0, start) + text + value.slice(end);
        setValue(next);
        requestAnimationFrame(() => {
          ta.focus();
          ta.selectionStart = ta.selectionEnd = start + text.length;
        });
      },
    }),
    [value]
  );

  const submit = () => {
    if (disabled || attaching) return;
    const filePaths = files.map((file) => file.path).join('\n');
    const text = files.length
      ? `${value ? `${value}\n\n` : ''}File attachments:\n${filePaths}`
      : value;
    if (text.trim() === '' && images.length === 0) return;
    onSend(text, images);
    setValue('');
    setImages([]);
    setFiles([]);
    setAttachmentError(null);
    textareaRef.current?.focus();
  };

  const attachFiles = async (selectedFiles: Iterable<File>) => {
    if (disabled || attaching) return;
    // Snapshot the selection before clearing the native file input.
    const selection = Array.from(selectedFiles);
    setAttaching(true);
    setAttachmentError(null);
    try {
      for (const file of selection) {
        try {
          if (file.type.startsWith('image/')) {
            const img = await readFileAsImage(file);
            setImages((prev) => [...prev, img]);
          } else {
            if (!workspaceId) throw new Error('Workspace is unavailable');
            if (file.size > 50 * 1024 * 1024) throw new Error('File exceeds 50 MiB');
            const attachment = await uploadWorkspaceAttachment(workspaceId, file);
            setFiles((prev) => [...prev, attachment]);
          }
        } catch (err) {
          setAttachmentError(`${file.name}: ${getErrorMessage(err, 'Failed to attach file')}`);
        }
      }
    } finally {
      setAttaching(false);
    }
  };

  return (
    <div className={styles.composer} data-testid="chat-composer">
      {(images.length > 0 || files.length > 0) && (
        <div className={styles.chips}>
          {images.map((img, i) => (
            <span className={styles.chip} key={i} data-testid="chat-image-chip">
              <img src={`data:${img.media_type};base64,${img.data}`} alt="attachment" />
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label="Remove image"
                onClick={() => setImages((prev) => prev.filter((_, j) => j !== i))}
              >
                ×
              </button>
            </span>
          ))}
          {files.map((file, i) => (
            <span className={styles.chip} key={file.path} data-testid="chat-file-chip">
              <span className={styles.fileName} title={file.path}>
                {file.name}
              </span>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label={`Remove ${file.name}`}
                onClick={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      {attaching && (
        <span className="text-muted" role="status">
          Attaching…
        </span>
      )}
      {attachmentError && (
        <div className="error-banner" role="alert">
          {attachmentError}
        </div>
      )}
      <div className={styles.composerRow}>
        <textarea
          ref={textareaRef}
          className={`textarea ${styles.input}`}
          data-testid="chat-input"
          rows={1}
          value={value}
          disabled={disabled}
          placeholder={
            ended
              ? 'Session ended. Restart to continue.'
              : disabled
                ? (disabledReason ?? 'Disconnected')
                : 'Message Claude…'
          }
          onChange={(e) => setValue(e.target.value)}
          onFocus={(e) => reportCaret(e.currentTarget)}
          onSelect={(e) => reportCaret(e.currentTarget)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          onPaste={(e) => {
            const files = e.clipboardData?.files;
            if (files && files.length > 0) {
              e.preventDefault();
              void attachFiles(files);
            }
          }}
        />
        <button
          type="button"
          className="btn btn--secondary btn--sm"
          disabled={disabled || attaching}
          onClick={() => fileRef.current?.click()}
        >
          Attach
        </button>
        <button
          type="button"
          className="btn btn--primary btn--sm"
          disabled={disabled || attaching}
          onClick={submit}
        >
          Send
        </button>
        <input
          ref={fileRef}
          type="file"
          multiple
          hidden
          data-testid="chat-file-input"
          onChange={(e) => {
            if (e.target.files) void attachFiles(e.target.files);
            e.target.value = '';
          }}
        />
      </div>
    </div>
  );
}
