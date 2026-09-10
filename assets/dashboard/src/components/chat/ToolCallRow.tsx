import { useState } from 'react';
import { operationForTool, type ActivityState } from '../../lib/chat/activity';
import styles from './chat.module.css';
import type { ToolSegment, SubCall } from '../../lib/chat/types';

type Summarizable = Pick<ToolSegment, 'name' | 'input' | 'inputJson'>;

export function summarizeTool(tool: Summarizable): string {
  const input = tool.input as Record<string, unknown> | null;
  if (input && typeof input === 'object') {
    for (const key of ['command', 'file_path', 'pattern']) {
      const v = input[key];
      if (typeof v === 'string' && v) return v;
    }
  }
  return tool.inputJson.slice(0, 80);
}

function firstLine(s: string): string {
  return s.split('\n', 1)[0];
}

// Lifecycle → ToolSegment state. The tool segment is built from the live
// wire events; a late task result (or tool_progress) updates the
// activity table without touching the segment. We map the activity
// lifecycle back to a state so the row reflects the latest result.
function lifecycleToState(lc: string): ToolSegment['state'] {
  if (lc === 'running' || lc === 'running-background' || lc === 'pending-input') return 'running';
  if (lc === 'preparing') return 'preparing';
  if (lc === 'finished') return 'done';
  return 'error';
}

const dotClass: Record<ToolSegment['state'], string> = {
  preparing: styles.toolDotPreparing,
  running: styles.toolDotRunning,
  done: styles.toolDotDone,
  error: styles.toolDotError,
};

const subDotClass: Record<SubCall['state'], string> = {
  preparing: styles.toolDotPreparing,
  running: styles.toolDotRunning,
  done: styles.toolDotDone,
  error: styles.toolDotError,
};

interface ToolCallRowProps {
  tool: ToolSegment;
  activity?: Pick<ActivityState, 'operations' | 'toolIndex'>;
}

export default function ToolCallRow({ tool, activity }: ToolCallRowProps) {
  const [expanded, setExpanded] = useState(false);
  const liveOp = tool.endedActivity ?? operationForTool(activity, tool.id);
  const state: ToolSegment['state'] = liveOp ? lifecycleToState(liveOp.lifecycle) : tool.state;
  // Keep the launch response in the details; a terminal notification is a
  // separate outcome and takes precedence in the compact result line.
  const lateResult = liveOp?.terminalAt ? liveOp.latestActivity : null;
  const displayResult = lateResult || tool.result;
  return (
    <div className={styles.tool} data-testid="chat-tool" data-tool-id={tool.id} tabIndex={-1}>
      <div
        className={styles.toolRow}
        data-testid="chat-tool-row"
        role="button"
        tabIndex={0}
        onClick={() => setExpanded((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setExpanded((v) => !v);
          }
        }}
      >
        <span
          className={`${styles.toolDot} ${dotClass[state]}`}
          data-testid="chat-tool-dot"
          data-state={state}
        />
        {state !== 'done' && <span className={styles.toolState}>{state}</span>}
        <span className={styles.toolName}>{tool.name}</span>
        <span className={styles.toolSummary} data-testid="chat-tool-summary">
          {summarizeTool(tool)}
        </span>
      </div>
      {displayResult && (
        <div className={styles.toolResult} data-testid="chat-tool-result">
          {firstLine(displayResult)}
        </div>
      )}
      {expanded && (
        <div className={styles.toolDetails} data-testid="chat-tool-details">
          <pre>{tool.inputJson}</pre>
          <pre>{tool.result}</pre>
          {lateResult && lateResult !== tool.result ? <pre>{lateResult}</pre> : null}
          {tool.subtools.length > 0 && (
            <div className={styles.subtools} data-testid="chat-tool-subtools">
              {tool.subtools.map((s) => (
                <div className={styles.subtool} data-testid="chat-tool-subtool" key={s.id}>
                  <span
                    className={`${styles.toolDot} ${subDotClass[s.state]}`}
                    data-state={s.state}
                  />
                  <span>{s.name}</span>
                  <span>{firstLine(s.result)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
