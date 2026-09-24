// Record and conversation model types for chat sessions. The record mirrors
// the daemon's conversation record (internal/chat); the conversation model is
// what the page renders.
import type { ActivityState, Operation } from './activity';

export type ChatProtocol = 'claude-stream-json' | 'codex-app-server';

export interface ChatImage {
  media_type: string;
  data: string;
  // Daemon-assigned path of the persisted /tmp copy; server → client only,
  // absent when persistence failed. Nothing in the UI reads it yet.
  path?: string;
}

export type HarnessLine = {
  type: string;
  subtype?: string;
  parent_tool_use_id?: string | null;
  [k: string]: unknown;
};

export type ConversationRecord =
  // text and images are omitempty on the Go side: an image-only paste has no
  // text key, and a text-only message has no images key.
  | { ts: string; type: 'user_message'; id: string; text?: string; images?: ChatImage[] }
  | { ts: string; type: 'user_message_dispatch'; id: string }
  | { ts: string; type: 'user_message_queue'; id: string; queued: boolean }
  | { ts: string; type: 'claude_takeover' }
  | { ts: string; type: 'control'; line: HarnessLine }
  | { ts: string; type: 'harness'; line: HarnessLine }
  | { ts: string; type: 'session'; event: 'ended' };

export interface UserMessage {
  kind: 'user';
  id: string;
  text: string;
  images: ChatImage[];
  queued: boolean;
}

// A user message shown inside an assistant turn: Codex folds a message sent
// mid-turn into the running turn (steer), so the page shows it where it landed.
interface UserSegment {
  kind: 'user';
  id: string;
  text: string;
  images: ChatImage[];
  queued: boolean;
}

interface ProseSegment {
  kind: 'prose';
  text: string;
  streaming: boolean;
}

interface ThinkingSegment {
  kind: 'thinking';
  text: string;
}

type ToolState = 'preparing' | 'running' | 'done' | 'error';

export interface SubCall {
  id: string;
  name: string;
  inputJson: string;
  result: string;
  state: ToolState;
}

export interface ToolSegment {
  // Frozen outcome from an ended process; independent of a replacement session.
  endedActivity?: Operation;
  kind: 'tool';
  id: string;
  name: string;
  input: unknown;
  inputJson: string;
  result: string;
  state: ToolState;
  subtools: SubCall[];
}

interface QuestionOption {
  label: string;
  description?: string;
}

export interface Question {
  id: string;
  question: string;
  header?: string;
  options: QuestionOption[];
  multiSelect?: boolean;
}

export interface PendingSegment {
  kind: 'pending';
  requestId: string;
  toolUseId: string;
  toolName: string;
  input: Record<string, unknown>;
  questions: Question[] | null;
  // true when schmux has no card for this request kind and can only decline it
  abortOnly?: boolean;
}

export interface AnsweredSegment {
  kind: 'answered';
  requestId: string;
  toolUseId: string;
  toolName: string;
  input: Record<string, unknown>;
  questions: Question[] | null;
  /** Display text per question id — exactly what was submitted. Empty
      when resolved without a known answer. */
  answers: Record<string, string>;
}

type TurnEnd = null | { state: 'done' } | { state: 'stopped' } | { state: 'error'; text: string };

export type Segment =
  ProseSegment | ThinkingSegment | ToolSegment | PendingSegment | AnsweredSegment | UserSegment;

export interface AssistantTurn {
  kind: 'assistant';
  segments: Segment[];
  end: TurnEnd;
  interrupted: boolean;
  thinking: boolean;
}

type ConversationItem = UserMessage | AssistantTurn;

export interface Conversation {
  items: ConversationItem[];
  phase: 'idle' | 'running';
  // Session-level activity and checklist. Lives outside any specific turn so
  // background tasks and pending inputs survive the turn that launched them.
  activity: ActivityState;
  // Internal flag for the Claude reducer: true after the first
  // user_message_dispatch marker lands. Drives whether a result opens
  // a follow-up turn for a held user_message (daemon-held mode) or
  // relies on the legacy isReplay echo (pre-dispatch-marker sessions).
  // Not part of any wire frame; do not serialize.
  claudeDaemonHeld?: boolean;
}
