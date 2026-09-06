import { useState } from 'react';
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

export default function ToolCallRow({ tool }: { tool: ToolSegment }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className={styles.tool} data-testid="chat-tool">
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
          className={`${styles.toolDot} ${dotClass[tool.state]}`}
          data-testid="chat-tool-dot"
          data-state={tool.state}
        />
        {tool.state !== 'done' && <span className={styles.toolState}>{tool.state}</span>}
        <span className={styles.toolName}>{tool.name}</span>
        <span className={styles.toolSummary} data-testid="chat-tool-summary">
          {summarizeTool(tool)}
        </span>
      </div>
      {tool.result && <div className={styles.toolResult}>{firstLine(tool.result)}</div>}
      {expanded && (
        <div className={styles.toolDetails} data-testid="chat-tool-details">
          <pre>{tool.inputJson}</pre>
          <pre>{tool.result}</pre>
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
