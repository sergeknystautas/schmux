import { useEffect, useRef, useState } from 'react';
import type { CuratorStreamEvent } from '../lib/types';

function CuratorEventRow({ event }: { event: CuratorStreamEvent }) {
  const raw = event.raw as Record<string, unknown>;

  if (event.event_type === 'curator_done') {
    const proposalId = raw.proposal_id as string | undefined;
    const fileCount = raw.file_count as number | undefined;
    return (
      <div className="curator-terminal__event curator-terminal__event--done">
        Done — proposal {proposalId} ({fileCount} files)
      </div>
    );
  }

  if (event.event_type === 'error' || event.event_type.endsWith('_error')) {
    // Two possible formats:
    // Wrapped:  {"type":"error","error":{"type":"server_error","message":"..."}}
    // Direct:   {"type":"server_error","detail":"Internal Server Error","status_code":500}
    const nested = raw.error as Record<string, unknown> | undefined;
    const errorType = (nested?.type as string) || (raw.type as string) || 'unknown';
    const detail =
      (nested?.message as string) ||
      (raw.detail as string) ||
      (raw.message as string) ||
      event.event_type;
    return (
      <div className="curator-terminal__event curator-terminal__event--error">
        LLM API error ({errorType}): {detail}
      </div>
    );
  }

  if (event.event_type === 'curator_error') {
    return (
      <div className="curator-terminal__event curator-terminal__event--error">
        Error: {(raw.error as string) || 'Unknown error'}
      </div>
    );
  }

  // API errors can arrive as assistant events with an "error" marker field
  if (event.event_type === 'assistant' && raw.error) {
    const message = raw.message as Record<string, unknown> | undefined;
    const content = message?.content as Array<Record<string, unknown>> | undefined;
    const errorText = content?.find((b) => b.type === 'text')?.text as string | undefined;
    return (
      <div className="curator-terminal__event curator-terminal__event--error">
        {errorText || 'Unknown API error'}
      </div>
    );
  }

  if (event.event_type === 'assistant') {
    const message = raw.message as Record<string, unknown> | undefined;
    if (!message) return null;
    const content = message.content as Array<Record<string, unknown>> | undefined;
    if (!content) return null;

    return (
      <>
        {content.map((block, i) => {
          if (block.type === 'text') {
            const text = block.text as string;
            // Truncate very long text blocks
            const display = text.length > 500 ? text.slice(0, 500) + '…' : text;
            return (
              <div key={i} className="curator-terminal__event curator-terminal__event--text">
                {display}
              </div>
            );
          }
          if (block.type === 'tool_use') {
            return (
              <div key={i} className="curator-terminal__event curator-terminal__event--tool">
                <span className="curator-terminal__tool-name">{block.name as string}</span>
              </div>
            );
          }
          if (block.type === 'thinking') {
            const text = block.thinking as string;
            const display = text && text.length > 300 ? text.slice(0, 300) + '…' : text;
            return (
              <div key={i} className="curator-terminal__event curator-terminal__event--thinking">
                {display}
              </div>
            );
          }
          return null;
        })}
      </>
    );
  }

  if (event.event_type === 'result') {
    const durationMs = raw.duration_ms as number | undefined;
    const durationSec = durationMs ? Math.round(durationMs / 1000) : '?';
    return (
      <div className="curator-terminal__event curator-terminal__event--result">
        Completed in {durationSec}s
      </div>
    );
  }

  // Skip system events and user (tool result) events — too noisy
  return null;
}

export default function CuratorTerminal({ events }: { events: CuratorStreamEvent[] }) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(true);

  useEffect(() => {
    if (expanded) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [events.length, expanded]);

  const visibleEvents = events.filter((e) => e.event_type !== 'system' && e.event_type !== 'user');

  if (visibleEvents.length === 0) return null;

  return (
    <div className="curator-terminal">
      <button className="curator-terminal__toggle" onClick={() => setExpanded((v) => !v)}>
        {expanded ? '▾' : '▸'} {visibleEvents.length} events
      </button>
      {expanded && (
        <div className="curator-terminal__body">
          {visibleEvents.map((event, i) => (
            <CuratorEventRow key={i} event={event} />
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}
