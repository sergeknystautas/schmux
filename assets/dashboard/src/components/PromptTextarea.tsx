import { useCallback, useEffect, useRef, useState } from 'react';
import useDebouncedCallback from '../hooks/useDebouncedCallback';

interface PromptTextareaProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  commands: string[];
  onSelectCommand: (command: string) => void;
  onSubmit?: () => void;
  'data-testid'?: string;
}

// Measure caret pixel coordinates inside a textarea using a mirror div
function getCaretCoordinates(
  textarea: HTMLTextAreaElement,
  position: number
): { top: number; left: number } {
  const mirror = document.createElement('div');
  const computed = getComputedStyle(textarea);

  // Copy styles that affect text layout
  const props = [
    'fontFamily',
    'fontSize',
    'fontWeight',
    'fontStyle',
    'lineHeight',
    'letterSpacing',
    'wordWrap',
    'whiteSpace',
    'tabSize',
    'paddingTop',
    'paddingRight',
    'paddingBottom',
    'paddingLeft',
    'borderTopWidth',
    'borderRightWidth',
    'borderBottomWidth',
    'borderLeftWidth',
    'boxSizing',
    'width',
  ];
  for (const prop of props) {
    mirror.style.setProperty(
      prop,
      computed.getPropertyValue(prop.replace(/([A-Z])/g, '-$1').toLowerCase())
    );
  }
  mirror.style.position = 'absolute';
  mirror.style.visibility = 'hidden';
  mirror.style.whiteSpace = 'pre-wrap';
  mirror.style.wordWrap = 'break-word';
  mirror.style.overflow = 'hidden';

  // Text before cursor
  mirror.appendChild(document.createTextNode(textarea.value.substring(0, position)));

  // Marker at cursor position
  const marker = document.createElement('span');
  marker.textContent = '\u200b';
  mirror.appendChild(marker);

  document.body.appendChild(mirror);
  const top = marker.offsetTop - textarea.scrollTop;
  const left = marker.offsetLeft;
  document.body.removeChild(mirror);

  return { top, left };
}

// Estimate line count: split by newlines, divide each by actual column width
function estimateLineCount(textarea: HTMLTextAreaElement): number {
  const computed = getComputedStyle(textarea);

  // Measure average character width
  const mirror = document.createElement('div');
  const props = ['fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing'];
  for (const prop of props) {
    mirror.style.setProperty(
      prop,
      computed.getPropertyValue(prop.replace(/([A-Z])/g, '-$1').toLowerCase())
    );
  }
  mirror.style.position = 'absolute';
  mirror.style.visibility = 'hidden';
  mirror.textContent = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  document.body.appendChild(mirror);
  const charWidth = mirror.offsetWidth / 46;
  document.body.removeChild(mirror);

  // Calculate actual cols from content width and char width
  const cols = Math.floor(textarea.clientWidth / charWidth);

  // Original algorithm: split by newlines, divide each line by cols
  const lines = textarea.value.split('\n');
  let count = 0;
  for (const line of lines) {
    count += Math.max(1, Math.ceil(line.length / cols));
  }

  return count;
}

export default function PromptTextarea({
  value,
  onChange,
  placeholder = 'Describe the task you want the targets to work on... (Type / for commands)',
  commands,
  onSelectCommand,
  onSubmit,
  'data-testid': dataTestId,
}: PromptTextareaProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const cursorPosRef = useRef(0);
  const slashStartRef = useRef(0);
  const [dismissed, setDismissed] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [menuPos, setMenuPos] = useState<{ top: number; left: number } | null>(null);
  const [expanded, setExpanded] = useState(false);

  // Debounced function to check if textarea should expand
  const checkShouldExpand = useDebouncedCallback(() => {
    if (textareaRef.current && !expanded && estimateLineCount(textareaRef.current) >= 3) {
      setExpanded(true);
    }
  }, 150);

  // Check on mount if draft content triggers expansion
  useEffect(() => {
    if (!expanded && textareaRef.current && value) {
      if (estimateLineCount(textareaRef.current) >= 3) {
        setExpanded(true);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // once on mount

  // Derive slash menu state from value + cursor position (avoids state batching issues)
  const beforeCursor = value.substring(0, cursorPosRef.current);
  const slashMatch = beforeCursor.match(/\/([\w ]*)$/);
  const slashActive =
    !dismissed &&
    !!slashMatch &&
    (slashMatch.index === 0 || /\s/.test(beforeCursor[slashMatch.index! - 1]));
  const slashQuery = slashActive ? slashMatch![1] : '';
  const slashStartPos = slashActive ? slashMatch!.index! : 0;

  // Keep ref in sync for selectCommand
  if (slashActive) {
    slashStartRef.current = slashStartPos;
  }

  const filteredCommands = commands
    .filter((cmd) => {
      const searchKey = cmd.startsWith('/') ? cmd : `/command ${cmd}`;
      const fullQuery = slashMatch ? `/${slashQuery}` : '';
      return searchKey.startsWith(fullQuery);
    })
    .slice(0, 8);

  const showMenu = slashActive && filteredCommands.length > 0;

  // Handle textarea input
  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    cursorPosRef.current = e.target.selectionStart;
    setDismissed(false);

    // Compute menu position from caret coordinates
    if (textareaRef.current) {
      const beforeText = e.target.value.substring(0, e.target.selectionStart);
      const match = beforeText.match(/\/([\w ]*)$/);
      if (match && (match.index === 0 || /\s/.test(beforeText[match.index! - 1]))) {
        const coords = getCaretCoordinates(textareaRef.current, match.index!);
        setMenuPos(coords);
      }
    }

    // Trigger debounced expansion check
    if (!expanded) {
      checkShouldExpand();
    }

    onChange(e.target.value);
  };

  // Handle keyboard navigation
  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Note: Cmd+Enter submission is handled globally by SpawnPage to work from any input

    if (!showMenu) return;

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelectedIndex((i) => (i + 1) % filteredCommands.length);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelectedIndex((i) => (i - 1 + filteredCommands.length) % filteredCommands.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      selectCommand(filteredCommands[selectedIndex]);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      setDismissed(true);
    }
  };

  // Select a command and remove the /query text
  const selectCommand = useCallback(
    (command: string) => {
      const beforeSlash = value.substring(0, slashStartRef.current);
      const afterCursor = value.substring(cursorPosRef.current);
      const newValue = beforeSlash + afterCursor;

      onChange(newValue);
      onSelectCommand(command);
      setDismissed(true);
    },
    [value, onChange, onSelectCommand]
  );

  // Handle clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (
        menuRef.current &&
        !menuRef.current.contains(e.target as Node) &&
        textareaRef.current &&
        !textareaRef.current.contains(e.target as Node)
      ) {
        setDismissed(true);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  return (
    <div style={{ position: 'relative' }}>
      <textarea
        ref={textareaRef}
        value={value}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        rows={expanded ? 20 : 5}
        className="textarea"
        autoFocus
        data-testid={dataTestId}
        style={{
          border: 'none',
          borderRadius: 'var(--radius-lg) var(--radius-lg) 0 0',
          resize: 'vertical',
          boxShadow: 'none',
        }}
      />
      {showMenu && menuPos && (
        <div
          ref={menuRef}
          style={{
            position: 'absolute',
            zIndex: 1000,
            top: menuPos.top + 24,
            left: menuPos.left,
            background: 'var(--color-surface)',
            border: '1px solid var(--color-border)',
            borderRadius: 'var(--radius-md)',
            boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
            minWidth: '200px',
            maxHeight: '300px',
            overflowY: 'auto',
          }}
        >
          {filteredCommands.map((cmd, index) => (
            <button
              key={cmd}
              type="button"
              onClick={() => selectCommand(cmd)}
              onMouseEnter={() => setSelectedIndex(index)}
              className={`btn${index === selectedIndex ? ' btn--primary' : ''}`}
              style={{
                display: 'block',
                width: '100%',
                padding: 'var(--spacing-sm) var(--spacing-md)',
                textAlign: 'left',
                border: 'none',
                cursor: 'pointer',
                fontSize: '0.875rem',
                borderRadius: 0,
              }}
            >
              {cmd.startsWith('/') ? cmd : `/command ${cmd}`}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
