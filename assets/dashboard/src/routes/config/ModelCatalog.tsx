import { useState, useMemo } from 'react';
import type { Model, RunnerInfo } from '../../lib/types';
import { sortModels } from '../../lib/modelSort';

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
  secretsModel: Model | null;
  isConfigured: boolean;
  isDefaults?: boolean;
};

function groupByProvider(models: Model[], runners: Record<string, RunnerInfo>): ProviderGroup[] {
  // Separate default models
  const defaults: Model[] = [];
  const nonDefaults: Model[] = [];
  for (const model of models) {
    if (model.is_default) {
      defaults.push(model);
    } else {
      nonDefaults.push(model);
    }
  }

  const groups = new Map<string, Model[]>();
  for (const model of nonDefaults) {
    const existing = groups.get(model.provider) || [];
    existing.push(model);
    groups.set(model.provider, existing);
  }

  const result: ProviderGroup[] = [];

  // Add defaults group first if there are any
  if (defaults.length > 0) {
    const hasDetectedRunner = defaults.some((m) => m.runners.some((r) => runners[r]?.available));
    result.push({
      provider: 'Defaults',
      models: sortModels(defaults),
      hasDetectedRunner,
      needsSecrets: false,
      secretsModel: null,
      isConfigured: false,
      isDefaults: true,
    });
  }

  for (const [provider, providerModels] of groups) {
    const hasDetectedRunner = providerModels.some((m) =>
      m.runners.some((r) => runners[r]?.available)
    );
    const secretsModel =
      providerModels.find((m) => m.required_secrets && m.required_secrets.length > 0) || null;
    const isConfigured = !!secretsModel && providerModels.some((m) => m.configured);
    const needsSecrets = !!secretsModel && !isConfigured;
    result.push({
      provider,
      models: sortModels(providerModels),
      hasDetectedRunner,
      needsSecrets,
      secretsModel,
      isConfigured,
      isDefaults: false,
    });
  }

  // Sort: providers with detected runners first, then alphabetical
  result.sort((a, b) => {
    // Keep defaults at top
    if (a.isDefaults !== b.isDefaults) return a.isDefaults ? -1 : 1;
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

function getProviderHint(group: ProviderGroup): string | null {
  if (group.isDefaults) return 'quick launch targets';
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
  const [expanded, setExpanded] = useState(group.hasDetectedRunner || group.isDefaults);
  const hint = getProviderHint(group);
  const canExpand = group.hasDetectedRunner || group.isDefaults;

  return (
    <div
      className={`model-catalog__provider${!group.hasDetectedRunner ? ' model-catalog__provider--disabled' : ''}`}
      data-disabled={!group.hasDetectedRunner}
    >
      <div className="model-catalog__provider-header">
        <button
          type="button"
          className="model-catalog__provider-toggle"
          onClick={() => canExpand && setExpanded(!expanded)}
          aria-expanded={expanded}
          disabled={!canExpand}
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
        {group.secretsModel && (
          <ProviderSecretsActions
            secretsModel={group.secretsModel}
            isConfigured={group.isConfigured}
            onModelAction={onModelAction}
          />
        )}
      </div>
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
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ProviderSecretsActions({
  secretsModel,
  isConfigured,
  onModelAction,
}: {
  secretsModel: Model;
  isConfigured: boolean;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
}) {
  if (isConfigured) {
    return (
      <div className="model-catalog__provider-actions">
        <button
          className="btn btn--sm btn--primary"
          onClick={() => onModelAction(secretsModel, 'update')}
        >
          Update Secrets
        </button>
        <button
          className="btn btn--sm btn--danger"
          onClick={() => onModelAction(secretsModel, 'remove')}
        >
          Remove
        </button>
      </div>
    );
  }
  return (
    <div className="model-catalog__provider-actions">
      <button
        className="btn btn--sm btn--primary"
        onClick={() => onModelAction(secretsModel, 'add')}
      >
        Add Secrets
      </button>
    </div>
  );
}

function ModelRow({
  model,
  runners,
  enabledModels,
  onToggleModel,
  onChangeRunner,
}: {
  model: Model;
  runners: Record<string, RunnerInfo>;
  enabledModels: Record<string, string>;
  onToggleModel: (modelId: string, enabled: boolean, defaultRunner: string) => void;
  onChangeRunner: (modelId: string, runner: string) => void;
}) {
  const detectedRunners = useMemo(() => getDetectedRunners(model, runners), [model, runners]);
  const isEnabled = model.id in enabledModels;
  const selectedRunner = enabledModels[model.id] || detectedRunners[0] || '';

  if (detectedRunners.length === 0) return null;

  // Format context window
  const contextWindowStr = model.context_window
    ? `${(model.context_window / 1000).toFixed(0)}K`
    : null;

  const handleRowClick = (e: React.MouseEvent) => {
    // Don't toggle when clicking the runner picker
    const target = e.target as HTMLElement;
    if (target.closest('.runner-picker')) return;
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
        {contextWindowStr && (
          <span className="model-catalog__model-context">{contextWindowStr}</span>
        )}
      </span>

      <RunnerPicker
        runners={detectedRunners}
        selected={selectedRunner}
        disabled={!isEnabled}
        onSelect={(runner) => onChangeRunner(model.id, runner)}
      />
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
