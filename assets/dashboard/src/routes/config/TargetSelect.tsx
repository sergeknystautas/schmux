import React from 'react';
import type { Model, RunTargetResponse } from '../../lib/types';

type TargetSelectProps = {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  includeDisabledOption?: boolean;
  includeNoneOption?: string;
  detectedTargets: RunTargetResponse[];
  models: Model[];
  promptableTargets: RunTargetResponse[];
  className?: string;
};

export default function TargetSelect({
  value,
  onChange,
  disabled,
  includeDisabledOption = true,
  includeNoneOption,
  detectedTargets,
  models,
  promptableTargets,
  className = 'input',
}: TargetSelectProps) {
  return (
    <select
      className={className}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
    >
      {includeDisabledOption && <option value="">Disabled</option>}
      {includeNoneOption && <option value="">{includeNoneOption}</option>}
      <optgroup label="Detected Tools">
        {detectedTargets.map((target) => (
          <option key={target.name} value={target.name}>
            {target.name}
          </option>
        ))}
      </optgroup>
      <optgroup label="Models">
        {models
          .filter((model) => model.configured)
          .map((model) => (
            <option key={model.id} value={model.id}>
              {model.display_name}
            </option>
          ))}
      </optgroup>
      <optgroup label="User Promptable">
        {promptableTargets.map((target) => (
          <option key={target.name} value={target.name}>
            {target.name}
          </option>
        ))}
      </optgroup>
    </select>
  );
}
