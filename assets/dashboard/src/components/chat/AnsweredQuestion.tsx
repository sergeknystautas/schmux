import styles from './chat.module.css';
import type { AnsweredSegment } from '../../lib/chat/types';

// Read-only record of a resolved question: the options as offered
// (chosen still primary, all disabled) plus the user's answer as a
// right-aligned bubble in the user-message style.
export default function AnsweredQuestion({ segment }: { segment: AnsweredSegment }) {
  const questions = segment.questions ?? [];
  return (
    <div className={`${styles.card} ${styles.cardQuestion}`} data-testid="chat-question-answered">
      {questions.map((q) => {
        const answer = segment.answers[q.id];
        const chosen = new Set((answer ?? '').split(', '));
        return (
          <div key={q.id}>
            {q.header && <div className={styles.questionHeader}>{q.header}</div>}
            <p className={styles.questionText}>{q.question}</p>
            <div className={styles.questionOptions}>
              {q.options.map((o) => {
                const active = chosen.has(o.label);
                return (
                  <button
                    key={o.label}
                    type="button"
                    disabled
                    aria-pressed={active}
                    className={`btn btn--sm ${active ? 'btn--primary' : 'btn--secondary'}`}
                  >
                    <span className={styles.questionOption}>
                      <span>{o.label}</span>
                      {o.description && (
                        <span className={styles.questionOptionDescription}>{o.description}</span>
                      )}
                    </span>
                  </button>
                );
              })}
            </div>
            {answer ? (
              <div className={styles.rowUser}>
                <div className={styles.bubble} data-testid="chat-question-answer">
                  {answer}
                </div>
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
