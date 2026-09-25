import { memo } from 'react';
import Tooltip from '../Tooltip';
import { useToast } from '../ToastProvider';
import { copyToClipboard } from '../../lib/utils';
import type { UserMessage } from '../../lib/chat/types';
import styles from './chat.module.css';
import { captureChatImageError, captureChatImageLoad } from '../../lib/chat/loadTelemetry';

function UserMessageBubbleInner({
  message,
  chatLoadProfiling = false,
}: {
  message: UserMessage;
  chatLoadProfiling?: boolean;
}) {
  const { success: toastSuccess, error: toastError } = useToast();
  const hasText = message.text.length > 0;

  const handleCopy = async () => {
    const copied = await copyToClipboard(message.text);
    if (copied) {
      toastSuccess('Copied message');
    } else {
      toastError('Failed to copy');
    }
  };

  return (
    <div className={styles.rowUser}>
      <div className={styles.bubble} data-testid="chat-user-message">
        {message.text}
        {message.images.map((img, i) => (
          <img
            key={i}
            src={img.preview_url ?? `data:${img.media_type};base64,${img.data}`}
            alt="attachment"
            loading={img.preview_url ? 'lazy' : undefined}
            width={img.preview_width}
            height={img.preview_height}
            onLoad={
              img.preview_url
                ? (event) => captureChatImageLoad(event.currentTarget, chatLoadProfiling)
                : undefined
            }
            onError={
              img.preview_url && chatLoadProfiling
                ? (event) => captureChatImageError(event.currentTarget)
                : undefined
            }
          />
        ))}
        {message.queued && (
          <span className={styles.queued} data-testid="chat-queued">
            queued
          </span>
        )}
        {hasText && (
          <Tooltip content="Copy message">
            <button
              type="button"
              className={`icon-btn ${styles.copyButton}`}
              aria-label="Copy message"
              onClick={handleCopy}
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                aria-hidden="true"
              >
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
              </svg>
            </button>
          </Tooltip>
        )}
      </div>
    </div>
  );
}

// Memoize: user messages are append-only and their object reference is
// stable, so a turn update re-renders only the turn, not every bubble above it.
const UserMessageBubble = memo(UserMessageBubbleInner);

export default UserMessageBubble;
