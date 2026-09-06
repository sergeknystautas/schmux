import { memo } from 'react';
import styles from './chat.module.css';
import type { UserMessage } from '../../lib/chat/types';

function UserMessageBubbleInner({ message }: { message: UserMessage }) {
  return (
    <div className={styles.rowUser}>
      <div className={styles.bubble} data-testid="chat-user-message">
        {message.text}
        {message.images.map((img, i) => (
          <img key={i} src={`data:${img.media_type};base64,${img.data}`} alt="attachment" />
        ))}
        {message.queued && (
          <span className={styles.queued} data-testid="chat-queued">
            queued
          </span>
        )}
      </div>
    </div>
  );
}

// Memoize: user messages are append-only and their object reference is
// stable, so a turn update re-renders only the turn, not every bubble above it.
const UserMessageBubble = memo(UserMessageBubbleInner);

export default UserMessageBubble;
