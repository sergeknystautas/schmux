import React from 'react';
import type { Model } from '../../lib/types';

type TargetSelectProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  includeDisabledOption?: boolean;
  includeNoneOption?: string;
  models: Model[];
  className?: string;
  id?: string;
};

export default function TargetSelect({
  value,
  onChange,
  disabled,
  includeDisabledOption = true,
  includeNoneOption,
  models,
  className = 'input',
  id,
}: TargetSelectProps) {
  const valueMissing = value !== '' && !models.some((m) => m.id === value);
  return (
    <select
      id={id}
      className={className}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
    >
      {includeDisabledOption && <option value="">Disabled</option>}
      {includeNoneOption && <option value="">{includeNoneOption}</option>}
      {models.map((model) => (
        <option key={model.id} value={model.id}>
          {model.display_name}
        </option>
      ))}
      {valueMissing && (
        <option value={value} disabled>
          {value} (unavailable)
        </option>
      )}
    </select>
  );
}
