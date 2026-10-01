import { memo } from 'react';
import CopyButton from '../CopyButton';
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
  const hasText = message.text.length > 0;

  return (
    <div className={styles.rowUser}>
      <div className={`${styles.bubble} hover-copy`} data-testid="chat-user-message">
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
          <CopyButton text={message.text} label="message" className="icon-btn hover-copy__btn" />
        )}
      </div>
    </div>
  );
}

// Memoize: user messages are append-only and their object reference is
// stable, so a turn update re-renders only the turn, not every bubble above it.
const UserMessageBubble = memo(UserMessageBubbleInner);

export default UserMessageBubble;
