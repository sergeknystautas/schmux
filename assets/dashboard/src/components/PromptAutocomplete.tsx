import { useMemo } from 'react';
import type { SpawnEntry, PromptHistoryEntry } from '../lib/types.generated';
import styles from './PromptAutocomplete.module.css';

export type { PromptHistoryEntry } from '../lib/types.generated';

export interface AutocompleteItem {
  text: string;
  source: 'spawn-entry' | 'history';
  meta?: string;
  spawnEntry?: SpawnEntry;
}

interface PromptAutocompleteProps {
  query: string;
  entries: SpawnEntry[];
  history: PromptHistoryEntry[];
  selectedIndex: number;
  onSelect: (item: AutocompleteItem) => void;
  onHover: (index: number) => void;
  style?: React.CSSProperties;
  items?: AutocompleteItem[];
}

function matchItems(
  query: string,
  entries: SpawnEntry[],
  history: PromptHistoryEntry[]
): AutocompleteItem[] {
  const q = query.toLowerCase();
  const results: AutocompleteItem[] = [];

  // Spawn entry templates: prefix matches first, then substring.
  const entryPrefix: AutocompleteItem[] = [];
  const entrySubstr: AutocompleteItem[] = [];
  for (const e of entries) {
    if (e.state !== 'pinned') continue;
    const template = e.prompt || e.name;
    const lower = template.toLowerCase();
    const item: AutocompleteItem = {
      text: template,
      source: 'spawn-entry',
      meta: e.use_count ? `${e.use_count}x` : undefined,
      spawnEntry: e,
    };
    if (lower.startsWith(q)) {
      entryPrefix.push(item);
    } else if (lower.includes(q)) {
      entrySubstr.push(item);
    }
  }
  results.push(...entryPrefix, ...entrySubstr);

  // Prompt history: prefix matches first, then substring.
  const histPrefix: AutocompleteItem[] = [];
  const histSubstr: AutocompleteItem[] = [];
  for (const h of history) {
    const lower = h.text.toLowerCase();
    // Skip if already covered by a spawn entry template.
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
  entries,
  history,
  selectedIndex,
  onSelect,
  onHover,
  style,
  items: externalItems,
}: PromptAutocompleteProps) {
  const computedItems = useMemo(
    () => matchItems(query, entries, history),
    [query, entries, history]
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
          aria-selected={i === selectedIndex}
        >
          <span
            className={`${styles.itemSource} ${item.source === 'history' ? styles.itemSourceHistory : ''}`}
          >
            {item.source === 'spawn-entry' ? 'action' : 'history'}
          </span>
          <span className={styles.itemText}>{item.text}</span>
          {item.meta && <span className={styles.itemMeta}>{item.meta}</span>}
        </button>
      ))}
    </div>
  );
}

export { matchItems };
