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
  return content.filter((block: { type: string }) => block.type === 'tool_use');
}

// Check if message is a permission request (tool_use_permission subtype)
function isPermissionRequest(message: StreamJsonMessage): boolean {
  return message.type === 'tool_use_permission' || message.subtype === 'tool_use_permission';
}

// Check if message is a result
function isResultMessage(message: StreamJsonMessage): boolean {
  return message.type === 'result';
}

// Extract a human-readable target from tool input (file path, command, etc.)
function getToolTarget(name: string, input: unknown): string | null {
  if (!input || typeof input !== 'object') return null;
  const inp = input as Record<string, unknown>;

  // Common patterns for different tools
  if (inp.file_path) return String(inp.file_path);
  if (inp.path) return String(inp.path);
  if (inp.command) {
    const cmd = String(inp.command);
    return cmd.length > 60 ? cmd.slice(0, 57) + '...' : cmd;
  }
  if (inp.pattern) return String(inp.pattern);
  if (inp.query) return String(inp.query);
  if (inp.url) return String(inp.url);

  return null;
}

// Inline tool use component - minimal, expandable
function ToolUseInline({ name, input, result }: { name: string; input: unknown; result?: string }) {
  const [open, setOpen] = useState(false);
  const target = getToolTarget(name, input);
  const hasResult = result !== undefined;
  const inputStr = typeof input === 'string' ? input : JSON.stringify(input, null, 2);

  return (
    <div>
      <div className="tool-use-inline" onClick={() => setOpen(!open)}>
        <span className={`tool-use-inline__icon${open ? ' tool-use-inline__icon--open' : ''}`}>
          &#x25B6;
        </span>
        <span className="tool-use-inline__name">{name}</span>
        {target && <span className="tool-use-inline__target">{target}</span>}
        <span
          className={`tool-use-inline__status ${
            hasResult ? 'tool-use-inline__status--success' : 'tool-use-inline__status--pending'
          }`}
        >
          {hasResult ? '✓' : '…'}
        </span>
      </div>
      {open && (
        <div className="tool-use-details">
          <div className="tool-use-details__section">
            <div className="tool-use-details__label">Input</div>
            <div className="tool-use-details__content">{inputStr}</div>
          </div>
          {result !== undefined && (
            <div className="tool-use-details__section">
              <div className="tool-use-details__label">Result</div>
              <div className="tool-use-details__content">
                {result.length > 2000 ? result.slice(0, 2000) + '...' : result}
              </div>
            </div>
          )}
        </div>
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
        Allow <span className="permission-prompt__tool">{toolName}</span>?
      </div>
      {description && <div className="permission-prompt__description">{description}</div>}
      <div className="permission-prompt__actions">
        <button
          className="btn btn--primary btn--sm"
          onClick={() => handleRespond(true)}
          disabled={responded}
        >
          Allow
        </button>
        <button
          className="btn btn--secondary btn--sm"
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
          disabled ? 'Waiting for agent...' : 'Send a follow-up message... (Cmd+Enter to send)'
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

export default function ConversationView({ sessionId, running }: ConversationViewProps) {
  const [messages, setMessages] = useState<StreamJsonMessage[]>([]);
  const [wsStatus, setWsStatus] = useState<'connecting' | 'connected' | 'disconnected'>(
    'connecting'
  );
  const [followTail, setFollowTail] = useState(true);
  const [showResume, setShowResume] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  // Track tool results by tool_use_id
  const toolResultsRef = useRef<Map<string, string>>(new Map());
  const [toolResults, setToolResults] = useState<Map<string, string>>(new Map());

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
              if (msg.type === 'content_block_start' && msg.content_block?.type === 'tool_result') {
                const id = msg.content_block.tool_use_id;
                const content = msg.content_block.content;
                if (id && content) {
                  results.set(id, typeof content === 'string' ? content : JSON.stringify(content));
                }
              }
              // Also check for tool_result type messages
              if (msg.type === 'tool_result') {
                const id = msg.tool_use_id;
                const content = msg.content;
                if (id && content) {
                  results.set(id, typeof content === 'string' ? content : JSON.stringify(content));
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
                typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content);
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
  const handleSendMessage = useCallback((content: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'user_message', content }));
    }
  }, []);

  // Send permission response
  const handlePermissionResponse = useCallback((requestId: string, approved: boolean) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(
        JSON.stringify({
          type: 'permission_response',
          request_id: requestId,
          approved,
        })
      );
    }
  }, []);

  // Determine if input should be disabled (during agent turn)
  const lastMessage = messages.length > 0 ? messages[messages.length - 1] : null;
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
          {text && <div className="conversation-message__content">{text}</div>}
          {toolUses.length > 0 && (
            <div className="tool-use-list">
              {toolUses.map((tool) => (
                <ToolUseInline
                  key={tool.id}
                  name={tool.name}
                  input={tool.input}
                  result={toolResults.get(tool.id)}
                />
              ))}
            </div>
          )}
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
            msg.description || (msg.tool?.input ? JSON.stringify(msg.tool.input, null, 2) : '')
          }
          requestId={msg.request_id || `perm-${index}`}
          onRespond={handlePermissionResponse}
        />
      );
    }

    // Result message
    if (isResultMessage(msg)) {
      const isError = msg.is_error || msg.subtype === 'error' || msg.result?.is_error;
      const costText = msg.cost_usd !== undefined ? ` · $${msg.cost_usd.toFixed(4)}` : '';
      return (
        <div
          key={index}
          className={`conversation-message conversation-message--result ${
            isError ? 'conversation-message--result-error' : 'conversation-message--result-success'
          }`}
        >
          {isError ? '✗ Error' : '✓ Done'}
          {msg.result?.text ? ` — ${msg.result.text}` : ''}
          {costText}
        </div>
      );
    }

    // System messages
    if (msg.type === 'system') {
      const text = typeof msg.message === 'string' ? msg.message : msg.message?.content || '';
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
          <div className="conversation-message conversation-message--system">
            Waiting for response...
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
