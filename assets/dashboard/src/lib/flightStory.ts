import type {
  FlightDetailResponse,
  FlightMessage,
  FlightMonitorAnswer,
  FlightMonitoredTurn,
  FlightOperation,
  FlightSessionAssignment,
} from './types.generated';
import { reviewWords, roleLabel } from './flights';

// The flight's story: one chronological list, newest first, of what happened
// to the flight, told in roles and outcomes rather than record fields. The
// operations, monitor turns, and monitor messages are the sources; the raw
// identifiers and full texts sit behind each entry for the user who needs to
// dig. Notifications and launch notices are the mechanism by which the pilot
// hears things and are not entries of their own, unless their delivery failed.

type StoryTone = 'neutral' | 'error';

type StoryField = { label: string; value: string; mono?: boolean };

type StoryTag = { text: string; tone: 'ok' | 'error' };

export type StoryEntry = {
  /** The operation, turn, or message id the entry comes from. */
  id: string;
  /** When it happened (RFC 3339). */
  at: string;
  /** One plain sentence. */
  headline: string;
  tone: StoryTone;
  /** A small pill beside the headline, such as the skill check. */
  tag?: StoryTag;
  /** The full response or message text, behind the expander. */
  text?: string;
  /** Raw identifiers and checks, behind the expander. */
  fields: StoryField[];
  /** Classifier answers with their confidence, behind the expander. */
  answers?: FlightMonitorAnswer[];
  /** The monitor item a Retry button re-runs, when the entry is retryable. */
  retryId?: string;
};

export type StoryFlight = Pick<FlightDetailResponse, 'sessions' | 'operations' | 'monitor'>;

/**
 * The name the story uses for each session: the role, numbered when the role
 * has had more than one session (a restart or a replaced reviewer), in the
 * order the sessions were started.
 */
export function actorNames(sessions: FlightSessionAssignment[] | undefined): Map<string, string> {
  const names = new Map<string, string>();
  for (const assignment of sessions ?? []) {
    const ids = [...(assignment.prior_session_ids ?? []), assignment.session_id].filter(
      (id): id is string => Boolean(id)
    );
    const label = roleLabel(assignment.role);
    ids.forEach((id, i) => names.set(id, ids.length > 1 ? `${label} ${i + 1}` : label));
  }
  return names;
}

const SNIPPET_LENGTH = 160;

/** The first line of a text, cut to a readable length for a headline. */
function snippet(text: string): string {
  const line = text.trim().split('\n')[0]?.trim() ?? '';
  return line.length > SNIPPET_LENGTH ? `${line.slice(0, SNIPPET_LENGTH - 1)}…` : line;
}

const PILOT_ROLE = 'pilot';

export function buildFlightStory(flight: StoryFlight): StoryEntry[] {
  const names = actorNames(flight.sessions);
  const roleOf = new Map<string, string>();
  for (const assignment of flight.sessions ?? []) {
    for (const id of [...(assignment.prior_session_ids ?? []), assignment.session_id]) {
      if (id) roleOf.set(id, assignment.role);
    }
  }
  const actor = (sessionId: string) => names.get(sessionId) ?? sessionId;

  const entries: StoryEntry[] = [];
  for (const op of flight.operations ?? []) {
    const entry = operationEntry(op);
    if (entry) entries.push(entry);
  }
  for (const turn of flight.monitor?.turns ?? []) {
    const role = roleOf.get(turn.session_id);
    const entry =
      role === PILOT_ROLE
        ? pilotTurnEntry(turn)
        : workerTurnEntry(turn, role, actor(turn.session_id));
    if (entry) entries.push(entry);
  }
  for (const msg of flight.monitor?.messages ?? []) {
    const entry = messageEntry(msg, actor);
    if (entry) entries.push(entry);
  }
  // Newest first; the sources are each appended in time order, so a tie keeps
  // the later source (messages after turns after operations) first.
  return entries
    .map((entry, index) => ({ entry, index, time: Date.parse(entry.at) }))
    .sort((a, b) => b.time - a.time || b.index - a.index)
    .map((x) => x.entry);
}

const OPERATION_HEADLINES: Record<string, { ok: string; failed: string }> = {
  takeoff: {
    ok: 'Took off: workspace allocated, pilot and planner started',
    failed: 'Takeoff failed',
  },
  execution: { ok: 'Entered execution: executor started', failed: 'Entering execution failed' },
  review: { ok: 'Entered review: reviewer started', failed: 'Entering review failed' },
  // A successful next_review is told by the pilot's next_reviewer message.
  next_review: { ok: '', failed: 'Starting another reviewer failed' },
  completion: { ok: 'Entered completion: committer started', failed: 'Entering completion failed' },
  done: { ok: 'Marked done', failed: 'Marking done failed' },
  dispose: { ok: 'Disposed', failed: 'Dispose failed' },
};

function operationEntry(op: FlightOperation): StoryEntry | null {
  const words = OPERATION_HEADLINES[op.action] ?? { ok: op.action, failed: `${op.action} failed` };
  const fields: StoryField[] = [
    { label: 'Operation', value: op.id, mono: true },
    { label: 'Action', value: op.action },
    { label: 'Status', value: op.status },
  ];
  for (const r of op.resources ?? []) {
    fields.push({
      label: r.kind,
      value: [r.id, r.status, r.error].filter(Boolean).join(' · '),
      mono: Boolean(r.id),
    });
  }
  if (op.status === 'failed') {
    return {
      id: op.id,
      at: op.created_at,
      headline: op.error ? `${words.failed}: ${op.error}` : words.failed,
      tone: 'error',
      fields,
    };
  }
  if (op.status === 'pending') {
    return {
      id: op.id,
      at: op.created_at,
      headline: `${words.ok || op.action}…`,
      tone: 'neutral',
      fields,
    };
  }
  if (!words.ok) return null;
  return { id: op.id, at: op.created_at, headline: words.ok, tone: 'neutral', fields };
}

function lastAttempt(turn: FlightMonitoredTurn) {
  const attempts = turn.attempts ?? [];
  return attempts.length > 0 ? attempts[attempts.length - 1] : undefined;
}

function turnFields(turn: FlightMonitoredTurn): StoryField[] {
  const fields: StoryField[] = [
    { label: 'Turn', value: turn.id, mono: true },
    { label: 'Session', value: turn.session_id, mono: true },
    { label: 'Records', value: `${turn.start_seq}–${turn.end_seq}` },
    { label: 'Outcome', value: turn.outcome },
    { label: 'Status', value: turn.status },
  ];
  if (turn.artifacts) {
    fields.push({
      label: 'Reported spec',
      value: turn.artifacts.spec_path || '(not reported)',
      mono: true,
    });
    fields.push({
      label: 'Reported plan',
      value: turn.artifacts.plan_path || '(not reported)',
      mono: true,
    });
    if (turn.artifacts.error)
      fields.push({ label: 'Spec/plan check', value: turn.artifacts.error });
  }
  if (turn.execution) {
    fields.push({ label: 'Skill check', value: turn.execution.skill });
    fields.push({
      label: 'Report check',
      value: turn.execution.report_verified
        ? `verified: ${turn.execution.report_path}`
        : turn.execution.report_error || 'not verified',
    });
  }
  if (turn.review) fields.push({ label: 'Skill check', value: turn.review.skill });
  for (const attempt of turn.attempts ?? []) {
    if (attempt.error) fields.push({ label: 'Classification', value: attempt.error });
  }
  return fields;
}

const ANSWER_WORDS: Record<string, Record<string, string>> = {
  attention: { needs_attention: 'needs attention' },
  planning_artifacts: {
    spec: 'claims the spec written',
    plan: 'claims the plan written',
    both: 'claims the spec and plan written',
  },
  execution_state: {
    finished: 'reports the plan implemented',
    stopped: 'stopped on a question or blocker',
    working: 'still working',
  },
  completion_state: {
    committed: 'commit made',
    gate_failed: 'a commit gate failed',
    working: 'still working',
    blocked: 'blocked',
  },
};

/** The classifier answers of a worker turn in words, plus the daemon's own checks. */
function turnWords(
  turn: FlightMonitoredTurn,
  role: string | undefined,
  answers: FlightMonitorAnswer[]
): string[] {
  const words: string[] = [];
  if (role === 'reviewer') {
    const fields: Record<string, string> = {};
    for (const a of answers) fields[a.id === 'review_state' ? 'state' : a.id] = a.choice;
    words.push(...reviewWords(fields));
  } else {
    for (const a of answers) {
      const word = ANSWER_WORDS[a.id]?.[a.choice];
      if (word) words.push(word);
    }
  }
  if (turn.artifacts) {
    words.push(turn.artifacts.error ? 'spec/plan check failed' : 'spec and plan verified');
  }
  if (turn.execution) {
    words.push(
      turn.execution.report_verified
        ? 'report verified'
        : turn.execution.report_error === 'not written yet'
          ? 'report not written yet'
          : 'report check failed'
    );
  }
  return words;
}

/** The skill evidence as a short pill: "<skill> invoked", "<skill> read", or "<skill> not found". */
function skillTag(skill: string | undefined): StoryTag | undefined {
  if (!skill) return undefined;
  const name = skill.split(' ')[0];
  if (skill.includes('not found')) return { text: `${name} not found`, tone: 'error' };
  if (skill.includes('invoked')) return { text: `${name} invoked`, tone: 'ok' };
  return { text: `${name} read`, tone: 'ok' };
}

function workerTurnEntry(
  turn: FlightMonitoredTurn,
  role: string | undefined,
  who: string
): StoryEntry | null {
  const base = {
    id: turn.id,
    at: turn.created_at,
    fields: turnFields(turn),
    text: turn.text || undefined,
  };
  if (turn.outcome === 'failed') {
    return {
      ...base,
      headline: `${who}'s turn failed${turn.error ? `: ${turn.error}` : ''}`,
      tone: 'error',
    };
  }
  if (turn.outcome === 'interrupted') {
    return { ...base, headline: `${who}'s turn was interrupted`, tone: 'error' };
  }
  const attempt = lastAttempt(turn);
  const answers = attempt?.answers ?? [];
  const tag = skillTag(turn.execution?.skill ?? turn.review?.skill);
  if (turn.status === 'failed') {
    return {
      ...base,
      headline: `${who} responded; classification failed${attempt?.error ? `: ${attempt.error}` : ''}`,
      tone: 'error',
      tag,
      retryId: turn.id,
    };
  }
  const words = turnWords(turn, role, answers);
  if (words.length === 0) {
    if (!turn.text.trim()) return null; // an empty turn is not part of the story
    const waiting = turn.status === 'pending' ? '; waiting for classification' : '';
    return { ...base, headline: `${who} responded${waiting}`, tone: 'neutral', tag };
  }
  return {
    ...base,
    headline: `${who}: ${words.join(', ')}`,
    tone: 'neutral',
    tag,
    answers: answers.length > 0 ? answers : undefined,
  };
}

/** A JSON decision, fenced or not, is told by the message it produced; prose is the pilot talking. */
function isDecision(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.startsWith('{') || trimmed.startsWith('```');
}

function pilotTurnEntry(turn: FlightMonitoredTurn): StoryEntry | null {
  const base = {
    id: turn.id,
    at: turn.created_at,
    fields: turnFields(turn),
    text: turn.text || undefined,
  };
  if (turn.status === 'failed') {
    const error = turn.outcome === 'completed' ? lastAttempt(turn)?.error : turn.error;
    return {
      ...base,
      headline: `Pilot's decision could not be applied${error ? `: ${error}` : ''}`,
      tone: 'error',
      retryId: turn.id,
    };
  }
  if (turn.outcome !== 'completed') {
    return {
      ...base,
      headline: `Pilot's turn ${turn.outcome}${turn.error ? `: ${turn.error}` : ''}`,
      tone: 'error',
    };
  }
  if (!turn.text.trim() || isDecision(turn.text)) return null;
  return { ...base, headline: `Pilot: ${snippet(turn.text)}`, tone: 'neutral' };
}

function messageFields(msg: FlightMessage, actor: (id: string) => string): StoryField[] {
  const fields: StoryField[] = [
    { label: 'Message', value: msg.id, mono: true },
    { label: 'Kind', value: msg.kind },
    {
      label: 'To',
      value: `${actor(msg.recipient_session_id)} (${msg.recipient_session_id})`,
      mono: true,
    },
    { label: 'Delivery', value: msg.status },
  ];
  if (msg.chat_message_id)
    fields.push({ label: 'Chat message', value: msg.chat_message_id, mono: true });
  if (msg.source_turn_id)
    fields.push({ label: 'From turn', value: msg.source_turn_id, mono: true });
  if (msg.error) fields.push({ label: 'Error', value: msg.error });
  return fields;
}

function messageEntry(msg: FlightMessage, actor: (id: string) => string): StoryEntry | null {
  const base = {
    id: msg.id,
    at: msg.created_at,
    fields: messageFields(msg, actor),
    text: msg.text,
  };
  const failed = msg.status === 'failed';
  const retryId = failed ? msg.id : undefined;
  const undelivered = failed ? ` (not delivered${msg.error ? `: ${msg.error}` : ''})` : '';
  switch (msg.kind) {
    case 'pilot_message':
      return {
        ...base,
        headline: `Pilot → ${actor(msg.recipient_session_id)}: ${snippet(msg.text)}${undelivered}`,
        tone: failed ? 'error' : 'neutral',
        retryId,
      };
    case 'next_reviewer':
      return {
        ...base,
        headline: `Pilot asked for another reviewer (${actor(msg.recipient_session_id)}): ${snippet(msg.text)}`,
        tone: 'neutral',
      };
    case 'pilot_stop':
      return { ...base, headline: `Pilot stopped the flight: ${snippet(msg.text)}`, tone: 'error' };
    case 'rejection':
      // Written by builds before 2026-10-28, which sent a decision error back
      // to the pilot. The failed pilot turn carries the reason; the message
      // is its echo.
      return failed
        ? {
            ...base,
            headline: `Returning a rejection to the Pilot failed: ${msg.error ?? ''}`,
            tone: 'error',
            retryId,
          }
        : null;
    default:
      // Notifications and launch notices are how the pilot hears things;
      // only a failed delivery is worth a line.
      return failed
        ? {
            ...base,
            headline: `Notifying the ${actor(msg.recipient_session_id)} failed: ${msg.error ?? ''}`,
            tone: 'error',
            retryId,
          }
        : null;
  }
}
