import React from 'react';

export type TargetOption = {
  id: string;
  label: string;
  source?: 'cli' | 'anthropic_api' | 'third_party_api' | 'ollama_api';
};

type TargetSelectProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  includeDisabledOption?: boolean;
  includeNoneOption?: string;
  options: TargetOption[];
  className?: string;
  id?: string;
};

export default function TargetSelect({
  value,
  onChange,
  disabled,
  includeDisabledOption = true,
  includeNoneOption,
  options,
  className = 'input',
  id,
}: TargetSelectProps) {
  const valueMissing = value !== '' && !options.some((o) => o.id === value);
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
      {options.map((o) => (
        <option key={o.id} value={o.id}>
          {o.label}
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
