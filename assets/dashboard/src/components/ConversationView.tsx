import React, { useCallback, useEffect, useRef, useState } from 'react';
import '../styles/conversation.css';
import type { StreamJsonMessage, StreamJsonWSServerMessage } from '../lib/types';

interface ConversationViewProps {
  sessionId: string;
  running: boolean;
}

// Extract text from a content value (string, array of content blocks, or nested)
function extractText(content: unknown): string {
  if (typeof content === 'string') return content;
  if (Array.isArray(content)) {
    return content
      .filter((block: { type?: string; text?: string }) => {
        // Accept text blocks, or blocks without a type that have text
        if (block.type === 'tool_use' || block.type === 'tool_result') return false;
        return typeof block.text === 'string';
      })
      .map((block: { text: string }) => block.text)
      .join('\n');
  }
  return '';
}

// Parse text content blocks from a message
// Handles multiple stream-json shapes:
//   { type: "user"|"assistant", message: { content: ... } }
//   { type: "user"|"assistant", content: ... }
//   { role: "user"|"assistant", content: ... }
function getTextContent(message: StreamJsonMessage): string {
  // Try message.message.content first (wrapped form)
  if (message.message?.content != null) {
    const text = extractText(message.message.content);
    if (text) return text;
  }
  // Try message.content directly (flat form)
  if (message.content != null) {
    const text = extractText(message.content);
    if (text) return text;
  }
  return '';
}

// Check if a message represents a user turn
function isUserMessage(message: StreamJsonMessage): boolean {
  if (message.type === 'user') return true;
  if (message.role === 'user') return true;
  if (message.message?.role === 'user') return true;
  return false;
}

// Check if a message represents an assistant turn
function isAssistantMessage(message: StreamJsonMessage): boolean {
  if (message.type === 'assistant') return true;
  if (message.role === 'assistant') return true;
  if (message.message?.role === 'assistant') return true;
  return false;
}

// Extract tool_use blocks from assistant message
function getToolUseBlocks(message: StreamJsonMessage): Array<{
  id: string;
  name: string;
  input: unknown;
}> {
  // Try message.message.content first, then message.content
  const content = message.message?.content ?? message.content;
  if (!Array.isArray(content)) return [];
  return content.filter(
    (block: { type: string }) => block.type === 'tool_use'
  );
}

// Check if message is a permission request (tool_use_permission subtype)
function isPermissionRequest(message: StreamJsonMessage): boolean {
  return message.type === 'tool_use_permission' || message.subtype === 'tool_use_permission';
}

// Check if message is a result
function isResultMessage(message: StreamJsonMessage): boolean {
  return message.type === 'result';
}

// Render a simple code block for tool input/output
function CodeBlock({ content }: { content: string }) {
  return (
    <pre style={{ margin: 0 }}>
      <code>{content}</code>
    </pre>
  );
}

// Tool use card component
function ToolUseCard({
  name,
  input,
  result,
}: {
  name: string;
  input: unknown;
  result?: string;
}) {
  const [open, setOpen] = useState(false);
  const inputStr =
    typeof input === 'string' ? input : JSON.stringify(input, null, 2);

  return (
    <div className="tool-use-card">
      <div className="tool-use-card__header" onClick={() => setOpen(!open)}>
        <span
          className={`tool-use-card__toggle${open ? ' tool-use-card__toggle--open' : ''}`}
        >
          &#x25B6;
        </span>
        <span className="tool-use-card__name">{name}</span>
      </div>
      {open && (
        <>
          <div className="tool-use-card__body">
            <div className="tool-use-card__section-label">Input</div>
            <CodeBlock content={inputStr} />
          </div>
          {result !== undefined && (
            <div className="tool-use-card__result">
              <div className="tool-use-card__section-label">Result</div>
              <CodeBlock content={result} />
            </div>
          )}
        </>
      )}
    </div>
  );
}

// Permission prompt component
function PermissionPrompt({
  toolName,
  description,
  requestId,
  onRespond,
}: {
  toolName: string;
  description: string;
  requestId: string;
  onRespond: (requestId: string, approved: boolean) => void;
}) {
  const [responded, setResponded] = useState(false);

  const handleRespond = (approved: boolean) => {
    setResponded(true);
    onRespond(requestId, approved);
  };

  return (
    <div className="permission-prompt">
      <div className="permission-prompt__header">
        Permission Required:{' '}
        <span className="permission-prompt__tool">{toolName}</span>
      </div>
      {description && (
        <div className="permission-prompt__description">{description}</div>
      )}
      <div className="permission-prompt__actions">
        <button
          className="btn btn--primary btn--sm"
          onClick={() => handleRespond(true)}
          disabled={responded}
        >
          Approve
        </button>
        <button
          className="btn btn--danger btn--sm"
          onClick={() => handleRespond(false)}
          disabled={responded}
        >
          Deny
        </button>
      </div>
    </div>
  );
}

// Message input bar
function MessageInput({
  disabled,
  onSend,
}: {
  disabled: boolean;
  onSend: (content: string) => void;
}) {
  const [value, setValue] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = () => {
    const trimmed = value.trim();
    if (!trimmed) return;
    onSend(trimmed);
    setValue('');
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleInput = () => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = `${textareaRef.current.scrollHeight}px`;
    }
  };

  return (
    <div className="conversation-input">
      <textarea
        ref={textareaRef}
        className="conversation-input__textarea"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={handleKeyDown}
        onInput={handleInput}
        placeholder={
          disabled
            ? 'Waiting for agent...'
            : 'Send a follow-up message... (Cmd+Enter to send)'
        }
        disabled={disabled}
        rows={1}
      />
      <button
        className="btn btn--primary conversation-input__send"
        onClick={handleSend}
        disabled={disabled || !value.trim()}
      >
        Send
      </button>
    </div>
  );
}

export default function ConversationView({
  sessionId,
  running,
}: ConversationViewProps) {
  const [messages, setMessages] = useState<StreamJsonMessage[]>([]);
  const [wsStatus, setWsStatus] = useState<
    'connecting' | 'connected' | 'disconnected'
  >('connecting');
  const [followTail, setFollowTail] = useState(true);
  const [showResume, setShowResume] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  // Track tool results by tool_use_id
  const toolResultsRef = useRef<Map<string, string>>(new Map());
  const [toolResults, setToolResults] = useState<Map<string, string>>(
    new Map()
  );

  // Connect to WebSocket
  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/streamjson/${sessionId}`;
    let ws: WebSocket;
    let reconnectTimeout: ReturnType<typeof setTimeout>;

    const connect = () => {
      setWsStatus('connecting');
      ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setWsStatus('connected');
      };

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as StreamJsonWSServerMessage;

          if (data.type === 'history') {
            setMessages(data.messages);
            // Process tool results from history
            const results = new Map<string, string>();
            for (const msg of data.messages) {
              if (
                msg.type === 'content_block_start' &&
                msg.content_block?.type === 'tool_result'
              ) {
                const id = msg.content_block.tool_use_id;
                const content = msg.content_block.content;
                if (id && content) {
                  results.set(
                    id,
                    typeof content === 'string'
                      ? content
                      : JSON.stringify(content)
                  );
                }
              }
              // Also check for tool_result type messages
              if (msg.type === 'tool_result') {
                const id = msg.tool_use_id;
                const content = msg.content;
                if (id && content) {
                  results.set(
                    id,
                    typeof content === 'string'
                      ? content
                      : JSON.stringify(content)
                  );
                }
              }
            }
            toolResultsRef.current = results;
            setToolResults(new Map(results));
          } else if (data.type === 'message') {
            const msg = data.message;
            setMessages((prev) => [...prev, msg]);
            // Check if this is a tool result
            if (msg.type === 'tool_result' && msg.tool_use_id && msg.content) {
              const content =
                typeof msg.content === 'string'
                  ? msg.content
                  : JSON.stringify(msg.content);
              toolResultsRef.current.set(msg.tool_use_id, content);
              setToolResults(new Map(toolResultsRef.current));
            }
          } else if (data.type === 'status') {
            // Status update - could trigger UI changes
          }
        } catch {
          // Ignore parse errors
        }
      };

      ws.onclose = () => {
        setWsStatus('disconnected');
        wsRef.current = null;
        // Reconnect after a delay
        reconnectTimeout = setTimeout(connect, 3000);
      };

      ws.onerror = () => {
        ws.close();
      };
    };

    connect();

    return () => {
      clearTimeout(reconnectTimeout);
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [sessionId]);

  // Auto-scroll
  useEffect(() => {
    if (followTail && messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages, followTail]);

  // Track scroll position for follow-tail
  const handleScroll = useCallback(() => {
    if (!messagesContainerRef.current) return;
    const el = messagesContainerRef.current;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
    setFollowTail(atBottom);
    setShowResume(!atBottom);
  }, []);

  const jumpToBottom = () => {
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
    setFollowTail(true);
    setShowResume(false);
  };

  // Send user message
  const handleSendMessage = useCallback(
    (content: string) => {
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(
          JSON.stringify({ type: 'user_message', content })
        );
      }
    },
    []
  );

  // Send permission response
  const handlePermissionResponse = useCallback(
    (requestId: string, approved: boolean) => {
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(
          JSON.stringify({
            type: 'permission_response',
            request_id: requestId,
            approved,
          })
        );
      }
    },
    []
  );

  // Determine if input should be disabled (during agent turn)
  const lastMessage =
    messages.length > 0 ? messages[messages.length - 1] : null;
  const isAgentTurn =
    running &&
    lastMessage !== null &&
    lastMessage.type !== 'result' &&
    lastMessage.type !== 'tool_use_permission' &&
    !(lastMessage.type === 'user');
  const inputDisabled = !running || (isAgentTurn && wsStatus === 'connected');

  // Render messages
  const renderMessage = (msg: StreamJsonMessage, index: number) => {
    // User message
    if (isUserMessage(msg)) {
      const text = getTextContent(msg);
      if (!text) return null;
      return (
        <div key={index} className="conversation-message conversation-message--user">
          <div className="conversation-message__role">You</div>
          <div className="conversation-message__content">{text}</div>
        </div>
      );
    }

    // Assistant message
    if (isAssistantMessage(msg)) {
      const text = getTextContent(msg);
      const toolUses = getToolUseBlocks(msg);
      if (!text && toolUses.length === 0) return null;
      return (
        <div key={index} className="conversation-message conversation-message--assistant">
          <div className="conversation-message__role">Claude</div>
          {text && (
            <div className="conversation-message__content">{text}</div>
          )}
          {toolUses.map((tool) => (
            <ToolUseCard
              key={tool.id}
              name={tool.name}
              input={tool.input}
              result={toolResults.get(tool.id)}
            />
          ))}
        </div>
      );
    }

    // Permission request
    if (isPermissionRequest(msg)) {
      return (
        <PermissionPrompt
          key={index}
          toolName={msg.tool?.name || msg.tool_name || 'Unknown tool'}
          description={
            msg.description ||
            (msg.tool?.input
              ? JSON.stringify(msg.tool.input, null, 2)
              : '')
          }
          requestId={msg.request_id || `perm-${index}`}
          onRespond={handlePermissionResponse}
        />
      );
    }

    // Result message
    if (isResultMessage(msg)) {
      const isError =
        msg.is_error || msg.subtype === 'error' || msg.result?.is_error;
      return (
        <div
          key={index}
          className={`conversation-message conversation-message--result ${
            isError
              ? 'conversation-message--result-error'
              : 'conversation-message--result-success'
          }`}
        >
          {isError ? 'Error' : 'Completed'}
          {msg.result?.text ? `: ${msg.result.text}` : ''}
          {msg.cost_usd !== undefined
            ? ` (cost: $${msg.cost_usd.toFixed(4)})`
            : ''}
        </div>
      );
    }

    // System messages
    if (msg.type === 'system') {
      const text =
        typeof msg.message === 'string'
          ? msg.message
          : msg.message?.content || '';
      if (!text) return null;
      return (
        <div key={index} className="conversation-message conversation-message--system">
          {text}
        </div>
      );
    }

    // Unknown message types - skip them
    return null;
  };

  return (
    <div className="conversation-view" style={{ position: 'relative' }}>
      <div
        className="conversation-view__messages"
        ref={messagesContainerRef}
        onScroll={handleScroll}
      >
        {messages.length === 0 && wsStatus === 'connected' && (
          <div
            style={{
              color: 'var(--color-text-muted)',
              fontStyle: 'italic',
              padding: 'var(--spacing-lg)',
            }}
          >
            Waiting for agent response...
          </div>
        )}
        {messages.map((msg, i) => renderMessage(msg, i))}
        <div ref={messagesEndRef} />
      </div>

      {showResume && (
        <button className="conversation-view__new-content" onClick={jumpToBottom}>
          Resume
        </button>
      )}

      <MessageInput disabled={inputDisabled} onSend={handleSendMessage} />
    </div>
  );
}
