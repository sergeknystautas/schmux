import styles from './AttachmentChips.module.css';
import type { ChatImage } from '../lib/chat/types';

interface AttachmentChipsProps {
  images: ChatImage[];
  files: { name: string; title?: string }[];
  attaching: boolean;
  error: string | null;
  onRemoveImage(index: number): void;
  onRemoveFile(index: number): void;
  /** Prefix for chip test ids, e.g. "chat" → chat-image-chip. */
  testIdPrefix: string;
  disabled?: boolean;
}

/** Image thumbnails, file-name chips, upload status, and upload errors. */
export default function AttachmentChips({
  images,
  files,
  attaching,
  error,
  onRemoveImage,
  onRemoveFile,
  testIdPrefix,
  disabled = false,
}: AttachmentChipsProps) {
  return (
    <>
      {(images.length > 0 || files.length > 0) && (
        <div className={styles.chips}>
          {images.map((img, i) => (
            <span className={styles.chip} key={i} data-testid={`${testIdPrefix}-image-chip`}>
              <img src={`data:${img.media_type};base64,${img.data}`} alt="attachment" />
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label="Remove image"
                disabled={disabled}
                onClick={() => onRemoveImage(i)}
              >
                ×
              </button>
            </span>
          ))}
          {files.map((file, i) => (
            <span
              className={styles.chip}
              key={`${i}-${file.title ?? file.name}`}
              data-testid={`${testIdPrefix}-file-chip`}
            >
              <span className={styles.fileName} title={file.title ?? file.name}>
                {file.name}
              </span>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                aria-label={`Remove ${file.name}`}
                disabled={disabled}
                onClick={() => onRemoveFile(i)}
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
      {error && (
        <div className="error-banner" role="alert">
          {error}
        </div>
      )}
    </>
  );
}
