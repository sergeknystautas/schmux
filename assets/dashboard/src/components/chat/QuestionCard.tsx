import { useState } from 'react';
import styles from './chat.module.css';
import type { PendingSegment, Question } from '../../lib/chat/types';

interface QuestionCardProps {
  pending: PendingSegment;
  onAnswer(
    requestId: string,
    answers: Record<string, string[]>,
    input: Record<string, unknown>
  ): void;
}

export default function QuestionCard({ pending, onAnswer }: QuestionCardProps) {
  const questions = pending.questions ?? [];
  const [selected, setSelected] = useState<Record<string, string[]>>({});
  const [other, setOther] = useState<Record<string, string>>({});

  const toggle = (q: Question, label: string) => {
    setSelected((prev) => {
      const cur = prev[q.id] ?? [];
      const next = q.multiSelect
        ? cur.includes(label)
          ? cur.filter((l) => l !== label)
          : [...cur, label]
        : [label];
      return { ...prev, [q.id]: next };
    });
  };

  const submit = () => {
    const answers: Record<string, string[]> = {};
    for (const q of questions) {
      const labels = selected[q.id] ?? [];
      const arr: string[] = labels.length > 0 ? labels : other[q.id] ? [other[q.id]] : [];
      answers[q.id] = arr;
    }
    onAnswer(pending.requestId, answers, pending.input);
  };

  return (
    <div className={styles.card} data-testid="chat-question-card">
      {questions.map((q) => (
        <div key={q.id}>
          {q.header && <div className={styles.questionHeader}>{q.header}</div>}
          <p className={styles.questionText}>{q.question}</p>
          <div className={styles.questionOptions}>
            {q.options.map((o) => {
              const active = (selected[q.id] ?? []).includes(o.label);
              return (
                <button
                  key={o.label}
                  type="button"
                  role={q.multiSelect ? 'checkbox' : 'radio'}
                  aria-pressed={active}
                  className={`btn btn--sm ${active ? 'btn--primary' : 'btn--secondary'}`}
                  onClick={() => toggle(q, o.label)}
                >
                  {o.label}
                </button>
              );
            })}
          </div>
          <div className={styles.otherRow}>
            <input
              type="text"
              className="input"
              placeholder="Other"
              aria-label={`Other ( ${q.question} )`}
              value={other[q.id] ?? ''}
              onChange={(e) => setOther((prev) => ({ ...prev, [q.id]: e.target.value }))}
            />
          </div>
        </div>
      ))}
      <div className={styles.cardActions}>
        <button type="button" className="btn btn--primary btn--sm" onClick={submit}>
          Submit
        </button>
      </div>
    </div>
  );
}
