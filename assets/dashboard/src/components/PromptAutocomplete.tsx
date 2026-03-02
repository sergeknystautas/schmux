import { useMemo } from 'react';
import type { Action } from '../lib/types.generated';
import type { PromptHistoryEntry } from '../lib/types.generated';
import styles from './PromptAutocomplete.module.css';

export interface AutocompleteItem {
  text: string;
  source: 'action' | 'history';
  meta?: string;
  action?: Action;
}

interface PromptAutocompleteProps {
  query: string;
  actions: Action[];
  history: PromptHistoryEntry[];
  selectedIndex: number;
  onSelect: (item: AutocompleteItem) => void;
  onHover: (index: number) => void;
  style?: React.CSSProperties;
  items?: AutocompleteItem[];
}

function matchItems(
  query: string,
  actions: Action[],
  history: PromptHistoryEntry[]
): AutocompleteItem[] {
  const q = query.toLowerCase();
  const results: AutocompleteItem[] = [];

  // Action templates: prefix matches first, then substring.
  const actionPrefix: AutocompleteItem[] = [];
  const actionSubstr: AutocompleteItem[] = [];
  for (const a of actions) {
    if (a.state !== 'pinned') continue;
    const template = a.template || a.name;
    const lower = template.toLowerCase();
    const item: AutocompleteItem = {
      text: template,
      source: 'action',
      meta: a.use_count ? `${a.use_count}x` : undefined,
      action: a,
    };
    if (lower.startsWith(q)) {
      actionPrefix.push(item);
    } else if (lower.includes(q)) {
      actionSubstr.push(item);
    }
  }
  results.push(...actionPrefix, ...actionSubstr);

  // Prompt history: prefix matches first, then substring.
  const histPrefix: AutocompleteItem[] = [];
  const histSubstr: AutocompleteItem[] = [];
  for (const h of history) {
    const lower = h.text.toLowerCase();
    // Skip if already covered by an action template.
    if (results.some((r) => r.text.toLowerCase() === lower)) continue;
    const item: AutocompleteItem = {
      text: h.text,
      source: 'history',
      meta: h.count > 1 ? `${h.count}x` : undefined,
    };
    if (lower.startsWith(q)) {
      histPrefix.push(item);
    } else if (lower.includes(q)) {
      histSubstr.push(item);
    }
  }
  results.push(...histPrefix, ...histSubstr);

  return results.slice(0, 8);
}

export default function PromptAutocomplete({
  query,
  actions,
  history,
  selectedIndex,
  onSelect,
  onHover,
  style,
  items: externalItems,
}: PromptAutocompleteProps) {
  const computedItems = useMemo(
    () => matchItems(query, actions, history),
    [query, actions, history]
  );
  const items = externalItems ?? computedItems;

  if (items.length === 0) return null;

  return (
    <div className={styles.overlay} style={style} data-testid="prompt-autocomplete">
      {items.map((item, i) => (
        <button
          key={`${item.source}-${item.text}`}
          type="button"
          className={`${styles.item} ${i === selectedIndex ? styles.itemSelected : ''}`}
          onClick={() => onSelect(item)}
          onMouseEnter={() => onHover(i)}
        >
          <span
            className={`${styles.itemSource} ${item.source === 'history' ? styles.itemSourceHistory : ''}`}
          >
            {item.source === 'action' ? 'action' : 'history'}
          </span>
          <span className={styles.itemText}>{item.text}</span>
          {item.meta && <span className={styles.itemMeta}>{item.meta}</span>}
        </button>
      ))}
    </div>
  );
}

export { matchItems };
