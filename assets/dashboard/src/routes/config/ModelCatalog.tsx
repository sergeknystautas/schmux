import { useState, useMemo } from 'react';
import type { Model, RunnerInfo } from '../../lib/types';

type ModelCatalogProps = {
  models: Model[];
  runners: Record<string, RunnerInfo>;
  enabledModels: Record<string, string>;
  onToggleModel: (modelId: string, enabled: boolean, defaultRunner: string) => void;
  onChangeRunner: (modelId: string, runner: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
};

type ProviderGroup = {
  provider: string;
  models: Model[];
  hasDetectedRunner: boolean;
  needsSecrets: boolean;
};

function groupByProvider(models: Model[], runners: Record<string, RunnerInfo>): ProviderGroup[] {
  const groups = new Map<string, Model[]>();
  for (const model of models) {
    const existing = groups.get(model.provider) || [];
    existing.push(model);
    groups.set(model.provider, existing);
  }

  const result: ProviderGroup[] = [];
  for (const [provider, providerModels] of groups) {
    const hasDetectedRunner = providerModels.some((m) =>
      m.runners.some((r) => runners[r]?.available)
    );
    const needsSecrets = providerModels.some(
      (m) => m.required_secrets && m.required_secrets.length > 0 && !m.configured
    );
    result.push({ provider, models: sortModels(providerModels), hasDetectedRunner, needsSecrets });
  }

  // Sort: providers with detected runners first, then alphabetical
  result.sort((a, b) => {
    if (a.hasDetectedRunner !== b.hasDetectedRunner) {
      return a.hasDetectedRunner ? -1 : 1;
    }
    return a.provider.localeCompare(b.provider);
  });

  return result;
}

function getDetectedRunners(model: Model, runners: Record<string, RunnerInfo>): string[] {
  return model.runners.filter((r) => runners[r]?.available).sort();
}

// Sort models by tier (haiku < sonnet < opus < other) then by version ascending.
// This produces: Haiku 3.5, Haiku 4.5, Sonnet 3.5, Sonnet 4, ..., Opus 4, Opus 4.6
const TIER_ORDER: Record<string, number> = { haiku: 0, sonnet: 1, opus: 2, flash: 0, pro: 1 };

function modelSortKey(model: Model): [number, number] {
  const name = model.display_name.toLowerCase();
  // Extract tier: last word before version number (e.g. "Claude Sonnet 4.6" → "sonnet")
  const parts = name.split(/\s+/);
  let tier = 99;
  let version = 0;
  for (const part of parts) {
    if (part in TIER_ORDER) tier = TIER_ORDER[part];
    const v = parseFloat(part);
    if (!isNaN(v) && v > 0) version = v;
  }
  return [tier, version];
}

function sortModels(models: Model[]): Model[] {
  return [...models].sort((a, b) => {
    const [aTier, aVer] = modelSortKey(a);
    const [bTier, bVer] = modelSortKey(b);
    if (aTier !== bTier) return aTier - bTier;
    return aVer - bVer;
  });
}

function getProviderHint(group: ProviderGroup): string | null {
  if (!group.hasDetectedRunner) return 'no tools detected';
  if (group.needsSecrets) return 'requires secrets';
  return null;
}

function ProviderSection({
  group,
  runners,
  enabledModels,
  onToggleModel,
  onChangeRunner,
  onModelAction,
}: {
  group: ProviderGroup;
  runners: Record<string, RunnerInfo>;
  enabledModels: Record<string, string>;
  onToggleModel: (modelId: string, enabled: boolean, defaultRunner: string) => void;
  onChangeRunner: (modelId: string, runner: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
}) {
  const [expanded, setExpanded] = useState(group.hasDetectedRunner);
  const hint = getProviderHint(group);

  return (
    <div
      className={`model-catalog__provider${!group.hasDetectedRunner ? ' model-catalog__provider--disabled' : ''}`}
      data-disabled={!group.hasDetectedRunner}
    >
      <button
        className="model-catalog__provider-header"
        onClick={() => group.hasDetectedRunner && setExpanded(!expanded)}
        aria-expanded={expanded}
      >
        <svg
          className={`model-catalog__provider-chevron${!expanded ? ' model-catalog__provider-chevron--collapsed' : ''}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
        {group.provider}
        {hint && <span className="model-catalog__provider-hint">{hint}</span>}
      </button>
      {expanded && (
        <div className="model-catalog__models">
          {group.models.map((model) => (
            <ModelRow
              key={model.id}
              model={model}
              runners={runners}
              enabledModels={enabledModels}
              onToggleModel={onToggleModel}
              onChangeRunner={onChangeRunner}
              onModelAction={onModelAction}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ModelRow({
  model,
  runners,
  enabledModels,
  onToggleModel,
  onChangeRunner,
  onModelAction,
}: {
  model: Model;
  runners: Record<string, RunnerInfo>;
  enabledModels: Record<string, string>;
  onToggleModel: (modelId: string, enabled: boolean, defaultRunner: string) => void;
  onChangeRunner: (modelId: string, runner: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
}) {
  const detectedRunners = useMemo(() => getDetectedRunners(model, runners), [model, runners]);
  const isEnabled = model.id in enabledModels;
  const selectedRunner = enabledModels[model.id] || detectedRunners[0] || '';

  if (detectedRunners.length === 0) return null;

  const needsSecrets =
    model.required_secrets && model.required_secrets.length > 0 && !model.configured;

  const handleRowClick = (e: React.MouseEvent) => {
    // Don't toggle when clicking runner picker buttons or secrets button
    const target = e.target as HTMLElement;
    if (target.closest('.runner-picker') || target.closest('.model-catalog__secrets-btn')) return;
    onToggleModel(model.id, !isEnabled, detectedRunners[0]);
  };

  return (
    <div className="model-catalog__model-row" onClick={handleRowClick} data-testid="model-row">
      <input
        type="checkbox"
        className="model-catalog__model-toggle"
        checked={isEnabled}
        onChange={(e) => onToggleModel(model.id, e.target.checked, detectedRunners[0])}
        onClick={(e) => e.stopPropagation()}
        aria-label={`Enable ${model.display_name}`}
      />
      <span
        className={`model-catalog__model-name${!isEnabled ? ' model-catalog__model-name--disabled' : ''}`}
      >
        {model.display_name}
      </span>

      <RunnerPicker
        runners={detectedRunners}
        selected={selectedRunner}
        disabled={!isEnabled}
        onSelect={(runner) => onChangeRunner(model.id, runner)}
      />

      {needsSecrets && (
        <button
          className="btn btn--sm btn--primary model-catalog__secrets-btn"
          onClick={() => onModelAction(model, model.configured ? 'update' : 'add')}
        >
          {model.configured ? 'Update' : 'Add'} Secrets
        </button>
      )}
    </div>
  );
}

function RunnerPicker({
  runners,
  selected,
  disabled,
  onSelect,
}: {
  runners: string[];
  selected: string;
  disabled: boolean;
  onSelect: (runner: string) => void;
}) {
  if (runners.length <= 1) {
    return (
      <div className="runner-picker runner-picker--single" data-testid="runner-picker">
        <span className="runner-picker__label">{runners[0]}</span>
      </div>
    );
  }

  return (
    <div
      className={`runner-picker${disabled ? ' runner-picker--disabled' : ''}`}
      data-testid="runner-picker"
      data-disabled={disabled}
    >
      {runners.map((runner) => (
        <button
          key={runner}
          className={`runner-picker__option${runner === selected ? ' runner-picker__option--selected' : ''}`}
          onClick={() => onSelect(runner)}
          disabled={disabled}
          data-testid="runner-option"
          data-selected={runner === selected}
        >
          {runner}
        </button>
      ))}
    </div>
  );
}

export default function ModelCatalog({
  models,
  runners,
  enabledModels,
  onToggleModel,
  onChangeRunner,
  onModelAction,
}: ModelCatalogProps) {
  const groups = useMemo(() => groupByProvider(models, runners), [models, runners]);

  return (
    <div className="model-catalog">
      {groups.map((group) => (
        <ProviderSection
          key={group.provider}
          group={group}
          runners={runners}
          enabledModels={enabledModels}
          onToggleModel={onToggleModel}
          onChangeRunner={onChangeRunner}
          onModelAction={onModelAction}
        />
      ))}
    </div>
  );
}
