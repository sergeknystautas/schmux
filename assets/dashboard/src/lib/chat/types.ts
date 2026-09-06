// Record and conversation model types for chat sessions. The record mirrors
// the daemon's conversation record (internal/chat); the conversation model is
// what the page renders.
export interface ChatImage {
  media_type: string;
  data: string;
}

export type HarnessLine = {
  type: string;
  subtype?: string;
  parent_tool_use_id?: string | null;
  [k: string]: unknown;
};

export type ConversationRecord =
  | { ts: string; type: 'user_message'; id: string; text: string; images?: ChatImage[] }
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
}

type Segment = ProseSegment | ThinkingSegment | ToolSegment | PendingSegment;

type TurnEnd = null | { state: 'done' } | { state: 'stopped' } | { state: 'error'; text: string };

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
}
