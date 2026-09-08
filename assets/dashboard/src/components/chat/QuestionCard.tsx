import { useRef, useState } from 'react';
import styles from './chat.module.css';
import type { PendingSegment, Question } from '../../lib/chat/types';
import type { QuestionAnswer } from '../../lib/chat-answers';
import type { ChatFocus } from '../../lib/chat-focus';

interface QuestionCardProps {
  pending: PendingSegment;
  onAnswer(
    requestId: string,
    answers: Record<string, string[]>,
    input: Record<string, unknown>
  ): void;
  /** In-progress answers restored for this request (per-session draft). */
  initialAnswers?: Record<string, QuestionAnswer>;
  /** Called with the full answer for a question whenever it changes. */
  onAnswerChange?(questionId: string, answer: QuestionAnswer): void;
  /** Called when focus lands on an Other input or an option button. */
  onFocusChange?(focus: ChatFocus): void;
}

export default function QuestionCard({
  pending,
  onAnswer,
  initialAnswers,
  onAnswerChange,
  onFocusChange,
}: QuestionCardProps) {
  const questions = pending.questions ?? [];
  const [selected, setSelected] = useState<Record<string, string[]>>(() => {
    const init: Record<string, string[]> = {};
    for (const q of questions) init[q.id] = initialAnswers?.[q.id]?.selected ?? [];
    return init;
  });
  const [other, setOther] = useState<Record<string, string>>(() => {
    const init: Record<string, string> = {};
    for (const q of questions) init[q.id] = initialAnswers?.[q.id]?.other ?? '';
    return init;
  });

  // Latest state for change reporting without re-registering callbacks.
  const stateRef = useRef({ selected, other });
  stateRef.current = { selected, other };

  const report = (questionId: string) => {
    const { selected: sel, other: oth } = stateRef.current;
    onAnswerChange?.(questionId, {
      selected: sel[questionId] ?? [],
      other: oth[questionId] ?? '',
    });
  };

  const toggle = (q: Question, label: string) => {
    const cur = selected[q.id] ?? [];
    const next = q.multiSelect
      ? cur.includes(label)
        ? cur.filter((l) => l !== label)
        : [...cur, label]
      : [label];
    const merged = { ...selected, [q.id]: next };
    stateRef.current = { ...stateRef.current, selected: merged };
    setSelected(merged);
    report(q.id);
  };

  const setOtherText = (q: Question, value: string) => {
    const merged = { ...other, [q.id]: value };
    stateRef.current = { ...stateRef.current, other: merged };
    setOther(merged);
    report(q.id);
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
                  onFocus={() =>
                    onFocusChange?.({
                      target: 'option',
                      requestId: pending.requestId,
                      questionId: q.id,
                      label: o.label,
                    })
                  }
                  data-chat-question-target
                  data-request-id={pending.requestId}
                  data-question-id={q.id}
                  data-option-label={o.label}
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
              onChange={(e) => setOtherText(q, e.target.value)}
              onFocus={(e) =>
                onFocusChange?.({
                  target: 'other-input',
                  requestId: pending.requestId,
                  questionId: q.id,
                  position: e.currentTarget.selectionStart ?? 0,
                })
              }
              onSelect={(e) =>
                onFocusChange?.({
                  target: 'other-input',
                  requestId: pending.requestId,
                  questionId: q.id,
                  position: e.currentTarget.selectionStart ?? 0,
                })
              }
              data-chat-question-target
              data-request-id={pending.requestId}
              data-question-id={q.id}
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
