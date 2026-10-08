import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import styles from './chat.module.css';
import type { ChatImage } from '../../lib/chat/types';
import type { ChatDraft } from '../../lib/chat-draft';
import type { WorkspaceAttachment } from '../../lib/types.generated';
import { uploadWorkspaceAttachment, getErrorMessage } from '../../lib/api';
import AttachmentChips from '../AttachmentChips';
import { useAttachments } from '../../hooks/useAttachments';
import { withFileAttachments } from '../../lib/attachments';
import { clientPerf } from '../../lib/clientPerf';

export interface ComposerHandle {
  focus(position?: number): void;
  insert(text: string): void;
  attachFiles(files: Iterable<File>): void;
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
  /** Reports whether Attach is currently usable for new file drops. */
  onAttachmentAvailabilityChange?(available: boolean): void;
  /** When set, the performance recording checkbox is shown with this start time. */
  recordingSince?: number | null;
  ref?: React.Ref<ComposerHandle>;
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
  onAttachmentAvailabilityChange,
  recordingSince,
  ref,
}: ComposerProps) {
  const [value, setValue] = useState(initialDraft?.text ?? '');
  const [attachRecording, setAttachRecording] = useState(true);
  const [recordingPath, setRecordingPath] = useState<string | null>(null);
  const [recordingError, setRecordingError] = useState<string | null>(null);
  const [uploadingRecording, setUploadingRecording] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const upload = useCallback(
    (file: File) => {
      if (!workspaceId) return Promise.reject(new Error('Workspace is unavailable'));
      return uploadWorkspaceAttachment(workspaceId, file);
    },
    [workspaceId]
  );
  const {
    images,
    files,
    attaching,
    error: attachmentError,
    attachFiles,
    removeImage,
    removeFile,
    clear: clearAttachments,
  } = useAttachments<WorkspaceAttachment>({
    upload,
    initialImages: initialDraft?.images,
    initialFiles: initialDraft?.files,
    disabled,
  });

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

  useLayoutEffect(() => {
    onAttachmentAvailabilityChange?.(!disabled && !attaching);
  }, [attaching, disabled, onAttachmentAvailabilityChange]);

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
      attachFiles: (selectedFiles: Iterable<File>) => {
        void attachFiles(selectedFiles);
      },
    }),
    [value, attachFiles]
  );

  const submit = async () => {
    if (disabled || attaching || uploadingRecording) return;
    let perfPath = recordingPath;
    const wantsRecording = recordingSince != null && attachRecording;
    if (wantsRecording && !perfPath && workspaceId) {
      setRecordingError(null);
      setUploadingRecording(true);
      try {
        const file = clientPerf.buildFile();
        const name = `client-perf-${file.builtAt}-${file.browserId}.json`;
        const uploaded = await uploadWorkspaceAttachment(
          workspaceId,
          new File([JSON.stringify(file)], name, { type: 'application/json' })
        );
        perfPath = uploaded.path;
        setRecordingPath(perfPath);
      } catch (err) {
        // The message and the recording stay where they were.
        setRecordingError(getErrorMessage(err, 'Failed to upload recording'));
        return;
      } finally {
        setUploadingRecording(false);
      }
    }
    const paths = files.map((file) => file.path);
    if (perfPath) paths.push(perfPath);
    const text = withFileAttachments(value, paths);
    if (text.trim() === '' && images.length === 0) return;
    try {
      onSend(text, images);
    } catch (err) {
      setRecordingError(getErrorMessage(err, 'Failed to send'));
      return;
    }
    if (perfPath) clientPerf.markSent();
    setRecordingPath(null);
    setValue('');
    clearAttachments();
    textareaRef.current?.focus();
  };

  return (
    <div className={styles.composer} data-testid="chat-composer">
      <AttachmentChips
        images={images}
        files={[
          ...files.map((file) => ({ name: file.name, title: file.path })),
          ...(recordingPath
            ? [{ name: recordingPath.split('/').pop() ?? 'recording', title: recordingPath }]
            : []),
        ]}
        attaching={attaching}
        error={attachmentError ?? recordingError}
        onRemoveImage={removeImage}
        onRemoveFile={(index) => {
          if (index >= files.length) setRecordingPath(null);
          else removeFile(index);
        }}
        testIdPrefix="chat"
      />
      {recordingSince != null && (
        <label className={`${styles.recordingToggle} flex-row gap-xs cursor-pointer`}>
          <input
            type="checkbox"
            checked={attachRecording}
            onChange={(e) => setAttachRecording(e.target.checked)}
          />
          {`Recording since ${new Date(recordingSince).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })} · attach`}
        </label>
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
