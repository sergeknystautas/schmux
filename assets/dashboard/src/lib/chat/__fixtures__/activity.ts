import background from './claude/activity-background.jsonl?raw';
import agent from './claude/activity-agent.jsonl?raw';
import type { ConversationRecord, HarnessLine } from '../types';

// Preserve the captured IDs and ordering; only the durable wrapper is synthetic.
export function capturedActivity(name: 'background' | 'agent'): ConversationRecord[] {
  const lines = { background, agent }[name]
    .trim()
    .split('\n')
    .map((line) => JSON.parse(line) as HarnessLine);
  const start = Date.parse('2026-01-01T00:00:00Z');
  return [
    {
      type: 'user_message',
      ts: new Date(start).toISOString(),
      id: 'capture-user',
      text: 'Run the captured task',
    },
    ...lines.map((line, i): ConversationRecord => ({
      type: 'harness',
      ts: new Date(start + (i + 1) * 1000).toISOString(),
      line,
    })),
  ];
}
