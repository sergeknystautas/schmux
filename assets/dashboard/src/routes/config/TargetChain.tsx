import React from 'react';
import TargetSelect from './TargetSelect';
import type { TargetOption } from './TargetSelect';

type TargetChainProps = {
  idPrefix: string;
  value: string[];
  onChange: (value: string[]) => void;
  options: TargetOption[];
};

/**
 * Ordered target chain editor: index 0 is the primary, later rows are
 * fallbacks used when a target returns 429. An empty fallback row means
 * "not configured" and is dropped on save.
 */
export default function TargetChain({ idPrefix, value, onChange, options }: TargetChainProps) {
  const addRow = () => onChange([...value, '']);
  const removeAt = (i: number) => onChange(value.filter((_, idx) => idx !== i));
  const setAt = (i: number, v: string) => onChange(value.map((t, idx) => (idx === i ? v : t)));

  return (
    <div className="target-chain">
      {value.map((t, i) => (
        <div className="form-group" key={`${idPrefix}-${i}`}>
          <label className="form-group__label" htmlFor={`${idPrefix}-${i}`}>
            {i === 0 ? 'Primary' : `Fallback ${i}`}
          </label>
          <div className="flex-row gap-md">
            <TargetSelect
              id={`${idPrefix}-${i}`}
              value={t}
              onChange={(v) => setAt(i, v)}
              options={options}
            />
            {i > 0 && (
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => removeAt(i)}
              >
                Remove
              </button>
            )}
          </div>
          {t !== '' && !options.some((o) => o.id === t) && (
            <p className="form-group__error">Selected target is not available.</p>
          )}
        </div>
      ))}
      <button type="button" className="btn btn--secondary btn--sm" onClick={addRow}>
        {value.length === 0 ? 'Add target' : 'Add fallback'}
      </button>
    </div>
  );
}
