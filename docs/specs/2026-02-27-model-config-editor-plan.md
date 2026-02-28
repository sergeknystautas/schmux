# Model Configuration Editor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the Sessions tab's first three sections (Detected Run Targets, Models, Promptable Targets) with a provider-grouped model configuration editor where users explicitly enable models and pick which tool runs each one.

**Architecture:** New `ModelCatalog` component replaces inline sections in `SessionsTab`. Provider groups are collapsible. Each model row has an enable toggle and a segmented runner picker. State flows through the existing `enabledModels: Record<string, string>` in useConfigForm, already wired to the backend. Backend catalog is expanded with older model versions and Google/Gemini models.

**Tech Stack:** React/TypeScript (frontend), Go (backend catalog expansion), Vitest + React Testing Library (tests)

---

## Context

Read these files before starting any task:

- `docs/specs/2026-02-27-model-config-editor-design.md` — the design this plan implements
- `assets/dashboard/src/routes/config/SessionsTab.tsx` — current component being replaced (286 lines)
- `assets/dashboard/src/routes/config/useConfigForm.ts` — state management (663 lines)
- `assets/dashboard/src/routes/ConfigPage.tsx:1259-1277` — how SessionsTab is wired
- `assets/dashboard/src/lib/types.generated.ts:213-222` — Model type, `375-379` — RunnerInfo type
- `internal/detect/models.go` — builtin model catalog (351 lines)
- `assets/dashboard/src/styles/global.css:2809-2845` — toggle switch CSS, `3935-3995` — item list CSS

---

### Task 1: Expand Backend Model Catalog

Add older Anthropic model versions and Google/Gemini models to `builtinModels`.

**Files:**

- Modify: `internal/detect/models.go:44-244` (builtinModels array)
- Test: `internal/detect/models_test.go`

**Step 1: Write failing test for new models**

Add to `internal/detect/models_test.go`:

```go
func TestExpandedCatalogModels(t *testing.T) {
	models := GetBuiltinModels()
	expectedIDs := []string{
		// Older Anthropic versions
		"claude-opus-4",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
		"claude-sonnet-3-5",
		"claude-haiku-3-5",
		// Google
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
	}
	modelIDs := make(map[string]bool, len(models))
	for _, m := range models {
		modelIDs[m.ID] = true
	}
	for _, id := range expectedIDs {
		if !modelIDs[id] {
			t.Errorf("expected model %q in catalog", id)
		}
	}
}

func TestGeminiModelsHaveGeminiRunner(t *testing.T) {
	for _, id := range []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"} {
		m, ok := FindModel(id)
		if !ok {
			t.Fatalf("model %q not found", id)
		}
		if _, ok := m.RunnerFor("gemini"); !ok {
			t.Errorf("model %q missing gemini runner", id)
		}
	}
}

func TestOlderClaudeModelsHaveBothRunners(t *testing.T) {
	for _, id := range []string{"claude-opus-4", "claude-sonnet-4-5", "claude-sonnet-4", "claude-sonnet-3-5", "claude-haiku-3-5"} {
		m, ok := FindModel(id)
		if !ok {
			t.Fatalf("model %q not found", id)
		}
		if _, ok := m.RunnerFor("claude"); !ok {
			t.Errorf("model %q missing claude runner", id)
		}
		if _, ok := m.RunnerFor("opencode"); !ok {
			t.Errorf("model %q missing opencode runner", id)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/detect/ -run TestExpandedCatalog -v`
Run: `go test ./internal/detect/ -run TestGeminiModels -v`
Run: `go test ./internal/detect/ -run TestOlderClaude -v`
Expected: FAIL — models not found

**Step 3: Add older Anthropic models to builtinModels**

In `internal/detect/models.go`, add after the existing Claude models (after line 75, before the third-party models comment):

```go
{
	ID:          "claude-opus-4",
	DisplayName: "Claude Opus 4",
	Provider:    "anthropic",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "claude-opus-4-20250514"},
		"opencode": {ModelValue: "anthropic/claude-opus-4-20250514"},
	},
},
{
	ID:          "claude-sonnet-4-5",
	DisplayName: "Claude Sonnet 4.5",
	Provider:    "anthropic",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "claude-sonnet-4-5-20250514"},
		"opencode": {ModelValue: "anthropic/claude-sonnet-4-5-20250514"},
	},
},
{
	ID:          "claude-sonnet-4",
	DisplayName: "Claude Sonnet 4",
	Provider:    "anthropic",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "claude-sonnet-4-20250514"},
		"opencode": {ModelValue: "anthropic/claude-sonnet-4-20250514"},
	},
},
{
	ID:          "claude-sonnet-3-5",
	DisplayName: "Claude Sonnet 3.5",
	Provider:    "anthropic",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "claude-3-5-sonnet-20241022"},
		"opencode": {ModelValue: "anthropic/claude-3-5-sonnet-20241022"},
	},
},
{
	ID:          "claude-haiku-3-5",
	DisplayName: "Claude Haiku 3.5",
	Provider:    "anthropic",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"claude":   {ModelValue: "claude-3-5-haiku-20241022"},
		"opencode": {ModelValue: "anthropic/claude-3-5-haiku-20241022"},
	},
},
```

**Step 4: Add Google/Gemini models to builtinModels**

Add after the OpenCode models section (after line 243, before the closing of builtinModels):

```go
// Google models
{
	ID:          "gemini-2.5-pro",
	DisplayName: "Gemini 2.5 Pro",
	Provider:    "google",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"gemini":   {ModelValue: "gemini-2.5-pro"},
		"opencode": {ModelValue: "google/gemini-2.5-pro"},
	},
},
{
	ID:          "gemini-2.5-flash",
	DisplayName: "Gemini 2.5 Flash",
	Provider:    "google",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"gemini":   {ModelValue: "gemini-2.5-flash"},
		"opencode": {ModelValue: "google/gemini-2.5-flash"},
	},
},
{
	ID:          "gemini-2.0-flash",
	DisplayName: "Gemini 2.0 Flash",
	Provider:    "google",
	Category:    "native",
	Runners: map[string]RunnerSpec{
		"gemini":   {ModelValue: "gemini-2.0-flash"},
		"opencode": {ModelValue: "google/gemini-2.0-flash"},
	},
},
```

**Step 5: Update TestGetBuiltinModels count**

Find the existing `TestGetBuiltinModels` in `models_test.go` and update the expected count from 16 to 24 (16 + 5 anthropic + 3 google).

**Step 6: Run all detect tests**

Run: `go test ./internal/detect/ -v`
Expected: ALL PASS

**Step 7: Commit**

Message: `feat(detect): expand model catalog with older Anthropic versions and Google models`

---

### Task 2: Add CSS for Model Catalog Component

Add styles for provider groups, model rows, segmented runner controls, and disabled states.

**Files:**

- Modify: `assets/dashboard/src/styles/global.css` (append after existing `.item-list` styles around line 3995)

**Step 1: Add model catalog CSS**

Append the following CSS to `global.css`:

```css
/* Model Catalog */
.model-catalog {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-sm);
}

.model-catalog__provider {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.model-catalog__provider--disabled {
  opacity: 0.5;
  pointer-events: none;
}

.model-catalog__provider-header {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm);
  padding: var(--spacing-sm) var(--spacing-md);
  background: var(--color-border-subtle);
  border: none;
  width: 100%;
  cursor: pointer;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--color-text);
  text-align: left;
}

.model-catalog__provider-header:hover {
  background: var(--color-border);
}

.model-catalog__provider-chevron {
  width: 12px;
  height: 12px;
  flex-shrink: 0;
  transition: transform var(--duration-fast) var(--easing);
}

.model-catalog__provider-chevron--collapsed {
  transform: rotate(-90deg);
}

.model-catalog__provider-hint {
  font-weight: 400;
  color: var(--color-text-muted);
  margin-left: auto;
  font-size: 0.8125rem;
}

.model-catalog__models {
  display: flex;
  flex-direction: column;
}

.model-catalog__model-row {
  display: flex;
  align-items: center;
  gap: var(--spacing-md);
  padding: var(--spacing-xs) var(--spacing-md);
  border-top: 1px solid var(--color-border);
}

.model-catalog__model-row:hover {
  background: var(--color-border-subtle);
}

.model-catalog__model-toggle {
  flex-shrink: 0;
}

.model-catalog__model-name {
  flex: 1;
  font-size: 0.875rem;
  color: var(--color-text);
  min-width: 0;
}

.model-catalog__model-name--disabled {
  color: var(--color-text-muted);
}

/* Runner segmented control */
.runner-picker {
  display: inline-flex;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  overflow: hidden;
  flex-shrink: 0;
}

.runner-picker__option {
  padding: 2px 8px;
  font-size: 0.75rem;
  font-family: var(--font-mono);
  border: none;
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
  transition: all var(--duration-fast) var(--easing);
}

.runner-picker__option:not(:last-child) {
  border-right: 1px solid var(--color-border);
}

.runner-picker__option--selected {
  background: var(--color-accent);
  color: white;
}

.runner-picker__option:hover:not(.runner-picker__option--selected) {
  background: var(--color-border-subtle);
}

.runner-picker--single {
  border: none;
}

.runner-picker--single .runner-picker__option {
  cursor: default;
  color: var(--color-text-muted);
  padding: 2px 0;
}

/* Secrets button in model row */
.model-catalog__secrets-btn {
  flex-shrink: 0;
}
```

**Step 2: Verify build**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds (CSS is valid)

**Step 3: Commit**

Message: `style: add model catalog CSS for provider groups and runner picker`

---

### Task 3: Build ModelCatalog Component

Create the new `ModelCatalog` component that renders the provider-grouped model editor.

**Files:**

- Create: `assets/dashboard/src/routes/config/ModelCatalog.tsx`
- Test: `assets/dashboard/src/routes/config/ModelCatalog.test.tsx`

**Step 1: Write the test file**

Create `assets/dashboard/src/routes/config/ModelCatalog.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ModelCatalog from './ModelCatalog';
import type { Model } from '../../lib/types';

const makeModel = (overrides: Partial<Model> & { id: string }): Model => ({
  display_name: overrides.id,
  provider: 'test',
  category: 'native',
  configured: false,
  runners: {},
  ...overrides,
});

const anthropicModels: Model[] = [
  makeModel({
    id: 'claude-opus-4-6',
    display_name: 'Claude Opus 4.6',
    provider: 'anthropic',
    runners: {
      claude: { available: true, configured: true },
      opencode: { available: true, configured: true },
    },
  }),
  makeModel({
    id: 'claude-sonnet-4-6',
    display_name: 'Claude Sonnet 4.6',
    provider: 'anthropic',
    runners: {
      claude: { available: true, configured: true },
      opencode: { available: false, configured: false },
    },
  }),
];

const codexModels: Model[] = [
  makeModel({
    id: 'gpt-5.3-codex',
    display_name: 'GPT 5.3 Codex',
    provider: 'openai',
    runners: {
      codex: { available: true, configured: true },
    },
  }),
];

const disabledModels: Model[] = [
  makeModel({
    id: 'gemini-2.5-pro',
    display_name: 'Gemini 2.5 Pro',
    provider: 'google',
    runners: {
      gemini: { available: false, configured: false },
    },
  }),
];

describe('ModelCatalog', () => {
  const defaultProps = {
    models: [...anthropicModels, ...codexModels, ...disabledModels],
    enabledModels: {} as Record<string, string>,
    onToggleModel: vi.fn(),
    onChangeRunner: vi.fn(),
    onModelAction: vi.fn(),
  };

  it('groups models by provider', () => {
    render(<ModelCatalog {...defaultProps} />);
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.getByText('google')).toBeInTheDocument();
  });

  it('renders model names', () => {
    render(<ModelCatalog {...defaultProps} />);
    expect(screen.getByText('Claude Opus 4.6')).toBeInTheDocument();
    expect(screen.getByText('GPT 5.3 Codex')).toBeInTheDocument();
  });

  it('shows only detected runners in picker', () => {
    render(<ModelCatalog {...defaultProps} />);
    // Claude Opus has both claude and opencode available
    const opusRow = screen.getByText('Claude Opus 4.6').closest('.model-catalog__model-row');
    expect(opusRow).toHaveTextContent('claude');
    expect(opusRow).toHaveTextContent('opencode');

    // Claude Sonnet has only claude available (opencode not available)
    const sonnetRow = screen.getByText('Claude Sonnet 4.6').closest('.model-catalog__model-row');
    expect(sonnetRow).toHaveTextContent('claude');
  });

  it('disables provider group when no tools detected', () => {
    render(<ModelCatalog {...defaultProps} />);
    const googleHeader = screen.getByText('google').closest('.model-catalog__provider');
    expect(googleHeader).toHaveClass('model-catalog__provider--disabled');
  });

  it('calls onToggleModel when checkbox changes', () => {
    const onToggleModel = vi.fn();
    render(<ModelCatalog {...defaultProps} onToggleModel={onToggleModel} />);
    const checkboxes = screen.getAllByRole('checkbox');
    fireEvent.click(checkboxes[0]);
    expect(onToggleModel).toHaveBeenCalledWith('claude-opus-4-6', true, 'claude');
  });

  it('shows checked state for enabled models', () => {
    render(<ModelCatalog {...defaultProps} enabledModels={{ 'claude-opus-4-6': 'claude' }} />);
    const checkboxes = screen.getAllByRole('checkbox');
    // First checkbox (claude-opus-4-6) should be checked
    expect(checkboxes[0]).toBeChecked();
  });

  it('highlights selected runner in segmented control', () => {
    render(<ModelCatalog {...defaultProps} enabledModels={{ 'claude-opus-4-6': 'opencode' }} />);
    const opusRow = screen.getByText('Claude Opus 4.6').closest('.model-catalog__model-row');
    const selectedBtn = opusRow?.querySelector('.runner-picker__option--selected');
    expect(selectedBtn).toHaveTextContent('opencode');
  });

  it('calls onChangeRunner when runner button clicked', () => {
    const onChangeRunner = vi.fn();
    render(
      <ModelCatalog
        {...defaultProps}
        enabledModels={{ 'claude-opus-4-6': 'claude' }}
        onChangeRunner={onChangeRunner}
      />
    );
    const opusRow = screen.getByText('Claude Opus 4.6').closest('.model-catalog__model-row');
    const opencodeBtn = Array.from(opusRow?.querySelectorAll('.runner-picker__option') || []).find(
      (btn) => btn.textContent === 'opencode'
    );
    if (opencodeBtn) fireEvent.click(opencodeBtn);
    expect(onChangeRunner).toHaveBeenCalledWith('claude-opus-4-6', 'opencode');
  });
});
```

**Step 2: Run tests to verify they fail**

Run: `cd assets/dashboard && npx vitest run src/routes/config/ModelCatalog.test.tsx`
Expected: FAIL — module not found

**Step 3: Create the ModelCatalog component**

Create `assets/dashboard/src/routes/config/ModelCatalog.tsx`:

```tsx
import { useState, useMemo } from 'react';
import type { Model } from '../../lib/types';

type ModelCatalogProps = {
  models: Model[];
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

function groupByProvider(models: Model[]): ProviderGroup[] {
  const groups = new Map<string, Model[]>();
  for (const model of models) {
    const existing = groups.get(model.provider) || [];
    existing.push(model);
    groups.set(model.provider, existing);
  }

  const result: ProviderGroup[] = [];
  for (const [provider, providerModels] of groups) {
    const hasDetectedRunner = providerModels.some((m) =>
      Object.values(m.runners || {}).some((r) => r.available)
    );
    const needsSecrets = providerModels.some((m) =>
      Object.values(m.runners || {}).some(
        (r) => r.available && r.required_secrets && r.required_secrets.length > 0 && !r.configured
      )
    );
    result.push({ provider, models: providerModels, hasDetectedRunner, needsSecrets });
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

function getDetectedRunners(model: Model): string[] {
  return Object.entries(model.runners || {})
    .filter(([, info]) => info.available)
    .map(([name]) => name)
    .sort();
}

function getProviderHint(group: ProviderGroup): string | null {
  if (!group.hasDetectedRunner) return 'no tools detected';
  if (group.needsSecrets) return 'requires secrets';
  return null;
}

function ProviderSection({
  group,
  enabledModels,
  onToggleModel,
  onChangeRunner,
  onModelAction,
}: {
  group: ProviderGroup;
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
  enabledModels,
  onToggleModel,
  onChangeRunner,
  onModelAction,
}: {
  model: Model;
  enabledModels: Record<string, string>;
  onToggleModel: (modelId: string, enabled: boolean, defaultRunner: string) => void;
  onChangeRunner: (modelId: string, runner: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
}) {
  const detectedRunners = useMemo(() => getDetectedRunners(model), [model]);
  const isEnabled = model.id in enabledModels;
  const selectedRunner = enabledModels[model.id] || detectedRunners[0] || '';

  if (detectedRunners.length === 0) return null;

  const needsSecrets = detectedRunners.some((runner) => {
    const info = model.runners[runner];
    return info?.required_secrets && info.required_secrets.length > 0 && !info.configured;
  });

  return (
    <div className="model-catalog__model-row">
      <input
        type="checkbox"
        className="model-catalog__model-toggle"
        checked={isEnabled}
        onChange={(e) => onToggleModel(model.id, e.target.checked, detectedRunners[0])}
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
  onSelect,
}: {
  runners: string[];
  selected: string;
  onSelect: (runner: string) => void;
}) {
  if (runners.length <= 1) {
    return (
      <div className="runner-picker runner-picker--single">
        <span className="runner-picker__option">{runners[0]}</span>
      </div>
    );
  }

  return (
    <div className="runner-picker">
      {runners.map((runner) => (
        <button
          key={runner}
          className={`runner-picker__option${runner === selected ? ' runner-picker__option--selected' : ''}`}
          onClick={() => onSelect(runner)}
        >
          {runner}
        </button>
      ))}
    </div>
  );
}

export default function ModelCatalog({
  models,
  enabledModels,
  onToggleModel,
  onChangeRunner,
  onModelAction,
}: ModelCatalogProps) {
  const groups = useMemo(() => groupByProvider(models), [models]);

  return (
    <div className="model-catalog">
      {groups.map((group) => (
        <ProviderSection
          key={group.provider}
          group={group}
          enabledModels={enabledModels}
          onToggleModel={onToggleModel}
          onChangeRunner={onChangeRunner}
          onModelAction={onModelAction}
        />
      ))}
    </div>
  );
}
```

**Step 4: Run tests**

Run: `cd assets/dashboard && npx vitest run src/routes/config/ModelCatalog.test.tsx`
Expected: ALL PASS

**Step 5: Commit**

Message: `feat(dashboard): add ModelCatalog component with provider grouping and runner picker`

---

### Task 4: Add enabledModels Actions to useConfigForm

Add `TOGGLE_MODEL` and `CHANGE_RUNNER` reducer actions so the model catalog can update `enabledModels` state directly instead of relying on the generic `SET_FIELD`.

**Files:**

- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts:215-242` (action types), `351-477` (reducer)
- Test: `assets/dashboard/src/routes/config/useConfigForm.test.ts`

**Step 1: Write failing test**

Add to `useConfigForm.test.ts`:

```typescript
it('TOGGLE_MODEL enables a model with default runner', () => {
  const { result } = renderHook(() => useConfigForm());
  act(() => {
    result.current.dispatch({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: true,
      defaultRunner: 'claude',
    });
  });
  expect(result.current.state.enabledModels).toEqual({ 'claude-opus-4-6': 'claude' });
});

it('TOGGLE_MODEL disables a model', () => {
  const { result } = renderHook(() => useConfigForm());
  act(() => {
    result.current.dispatch({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: true,
      defaultRunner: 'claude',
    });
    result.current.dispatch({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: false,
      defaultRunner: 'claude',
    });
  });
  expect(result.current.state.enabledModels).toEqual({});
});

it('CHANGE_RUNNER updates runner for enabled model', () => {
  const { result } = renderHook(() => useConfigForm());
  act(() => {
    result.current.dispatch({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: true,
      defaultRunner: 'claude',
    });
    result.current.dispatch({
      type: 'CHANGE_RUNNER',
      modelId: 'claude-opus-4-6',
      runner: 'opencode',
    });
  });
  expect(result.current.state.enabledModels).toEqual({ 'claude-opus-4-6': 'opencode' });
});
```

**Step 2: Run test to verify it fails**

Run: `cd assets/dashboard && npx vitest run src/routes/config/useConfigForm.test.ts -t "TOGGLE_MODEL"`
Expected: FAIL — action type not recognized

**Step 3: Add action types to the union**

In `useConfigForm.ts`, add to the `ConfigFormAction` union type (around line 232):

```typescript
| { type: 'TOGGLE_MODEL'; modelId: string; enabled: boolean; defaultRunner: string }
| { type: 'CHANGE_RUNNER'; modelId: string; runner: string }
```

**Step 4: Add reducer cases**

In the reducer function (around line 354), add cases:

```typescript
case 'TOGGLE_MODEL': {
  const next = { ...state.enabledModels };
  if (action.enabled) {
    next[action.modelId] = action.defaultRunner;
  } else {
    delete next[action.modelId];
  }
  return { ...state, enabledModels: next };
}
case 'CHANGE_RUNNER': {
  if (!(action.modelId in state.enabledModels)) return state;
  return {
    ...state,
    enabledModels: { ...state.enabledModels, [action.modelId]: action.runner },
  };
}
```

**Step 5: Run tests**

Run: `cd assets/dashboard && npx vitest run src/routes/config/useConfigForm.test.ts`
Expected: ALL PASS

**Step 6: Commit**

Message: `feat(dashboard): add TOGGLE_MODEL and CHANGE_RUNNER reducer actions`

---

### Task 5: Replace SessionsTab Sections with ModelCatalog

Gut the first 3 sections of SessionsTab and wire in ModelCatalog. Update ConfigPage props.

**Files:**

- Modify: `assets/dashboard/src/routes/config/SessionsTab.tsx` (full rewrite of component)
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx:1259-1277` (update props)
- Modify: `assets/dashboard/src/routes/config/SessionsTab.test.tsx` (update tests)

**Step 1: Rewrite SessionsTab**

Replace the contents of `assets/dashboard/src/routes/config/SessionsTab.tsx` with:

```tsx
import React from 'react';
import type { ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';
import ModelCatalog from './ModelCatalog';

type SessionsTabProps = {
  models: Model[];
  enabledModels: Record<string, string>;
  commandTargets: { name: string; command: string; source?: string }[];
  newCommandName: string;
  newCommandCommand: string;
  dispatch: React.Dispatch<ConfigFormAction>;
  onAddCommand: () => void;
  onRemoveCommand: (name: string) => void;
  onModelAction: (model: Model, mode: 'add' | 'remove' | 'update') => void;
  onOpenRunTargetEditModal: (target: { name: string; command: string; source?: string }) => void;
};

export default function SessionsTab({
  models,
  enabledModels,
  commandTargets,
  newCommandName,
  newCommandCommand,
  dispatch,
  onAddCommand,
  onRemoveCommand,
  onModelAction,
  onOpenRunTargetEditModal,
}: SessionsTabProps) {
  const handleToggleModel = (modelId: string, enabled: boolean, defaultRunner: string) => {
    dispatch({ type: 'TOGGLE_MODEL', modelId, enabled, defaultRunner });
  };

  const handleChangeRunner = (modelId: string, runner: string) => {
    dispatch({ type: 'CHANGE_RUNNER', modelId, runner });
  };

  return (
    <div className="wizard-step-content" data-step="2">
      <h2 className="wizard-step-content__title">Models</h2>
      <p className="wizard-step-content__description">
        Enable models and choose which tool runs each one. Only enabled models appear in the spawn
        wizard.
      </p>

      <ModelCatalog
        models={models}
        enabledModels={enabledModels}
        onToggleModel={handleToggleModel}
        onChangeRunner={handleChangeRunner}
        onModelAction={onModelAction}
      />

      <h3>Command Targets</h3>
      <p className="section-hint">
        Shell commands you want to run quickly, like launching a terminal or starting the app.
      </p>
      {commandTargets.length === 0 ? (
        <div className="empty-state-hint">
          No command targets configured. These run without prompts.
        </div>
      ) : (
        <div className="item-list item-list--two-col">
          {commandTargets.map((cmd) => (
            <div className="item-list__item" key={cmd.name}>
              <div className="item-list__item-primary item-list__item-row">
                <span className="item-list__item-name">{cmd.name}</span>
                <span className="item-list__item-detail item-list__item-detail--wide">
                  {cmd.command}
                </span>
              </div>
              {cmd.source === 'user' ? (
                <div className="btn-group">
                  <button
                    className="btn btn--sm btn--primary"
                    onClick={() => onOpenRunTargetEditModal(cmd)}
                  >
                    Edit
                  </button>
                  <button
                    className="btn btn--sm btn--danger"
                    onClick={() => onRemoveCommand(cmd.name)}
                  >
                    Remove
                  </button>
                </div>
              ) : (
                <button
                  className="btn btn--sm btn--danger"
                  onClick={() => onRemoveCommand(cmd.name)}
                >
                  Remove
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      <div className="add-item-form">
        <div className="add-item-form__inputs">
          <input
            type="text"
            className="input"
            placeholder="Name"
            value={newCommandName}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newCommandName', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddCommand()}
          />
          <input
            type="text"
            className="input"
            placeholder="Command (e.g., go build ./...)"
            value={newCommandCommand}
            onChange={(e) =>
              dispatch({ type: 'SET_FIELD', field: 'newCommandCommand', value: e.target.value })
            }
            onKeyDown={(e) => e.key === 'Enter' && onAddCommand()}
          />
        </div>
        <button type="button" className="btn btn--sm btn--primary" onClick={onAddCommand}>
          Add
        </button>
      </div>
    </div>
  );
}
```

**Step 2: Update ConfigPage.tsx props**

In `assets/dashboard/src/routes/ConfigPage.tsx`, find the SessionsTab rendering (lines 1259-1277) and replace with:

```tsx
{
  currentTab === 2 && (
    <SessionsTab
      models={state.models}
      enabledModels={state.enabledModels}
      commandTargets={state.commandTargets}
      newCommandName={state.newCommandName}
      newCommandCommand={state.newCommandCommand}
      dispatch={dispatch}
      onAddCommand={addCommand}
      onRemoveCommand={removeCommand}
      onModelAction={handleModelAction}
      onOpenRunTargetEditModal={openRunTargetEditModal}
    />
  );
}
```

Remove these props that are no longer passed: `detectedTargets`, `promptableTargets`, `newPromptableName`, `newPromptableCommand`, `onAddPromptableTarget`, `onRemovePromptableTarget`.

**Step 3: Check for removed callback functions**

Search ConfigPage.tsx for `addPromptableTarget` and `removePromptableTarget` function definitions. If they are only used by SessionsTab (no other tab references them), leave them in place for now — they won't cause errors, just unused code. If the linter flags them, remove the functions.

**Step 4: Update SessionsTab.test.tsx**

Replace `assets/dashboard/src/routes/config/SessionsTab.test.tsx` with:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import SessionsTab from './SessionsTab';
import type { Model } from '../../lib/types';

const models: Model[] = [
  {
    id: 'claude-opus-4-6',
    display_name: 'Claude Opus 4.6',
    provider: 'anthropic',
    category: 'native',
    configured: true,
    runners: {
      claude: { available: true, configured: true },
      opencode: { available: true, configured: true },
    },
  },
  {
    id: 'gpt-5.3-codex',
    display_name: 'GPT 5.3 Codex',
    provider: 'openai',
    category: 'native',
    configured: true,
    runners: {
      codex: { available: true, configured: true },
    },
  },
];

const commandTargets = [{ name: 'build', command: 'go build ./...', source: 'user' }];

const dispatch = vi.fn();

const defaultProps = {
  models,
  enabledModels: {} as Record<string, string>,
  commandTargets,
  newCommandName: '',
  newCommandCommand: '',
  dispatch,
  onAddCommand: vi.fn(),
  onRemoveCommand: vi.fn(),
  onModelAction: vi.fn(),
  onOpenRunTargetEditModal: vi.fn(),
};

describe('SessionsTab', () => {
  it('renders model catalog with provider groups', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByText('openai')).toBeInTheDocument();
  });

  it('renders command targets section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Command Targets')).toBeInTheDocument();
    expect(screen.getByText('build')).toBeInTheDocument();
  });

  it('dispatches TOGGLE_MODEL when model checkbox clicked', () => {
    render(<SessionsTab {...defaultProps} />);
    const checkboxes = screen.getAllByRole('checkbox');
    fireEvent.click(checkboxes[0]);
    expect(dispatch).toHaveBeenCalledWith({
      type: 'TOGGLE_MODEL',
      modelId: 'claude-opus-4-6',
      enabled: true,
      defaultRunner: 'claude',
    });
  });

  it('renders add command form', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByPlaceholderText('Name')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Command (e.g., go build ./...)')).toBeInTheDocument();
  });

  it('does not render detected targets or promptable targets sections', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.queryByText('Detected Run Targets')).not.toBeInTheDocument();
    expect(screen.queryByText('Promptable Targets')).not.toBeInTheDocument();
  });
});
```

**Step 5: Run all frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS. If other test files reference old SessionsTab props (like `SpawnPage.agent-select.test.tsx`), fix those too — they may reference `promptableTargets` in config mock data which is fine since it's in the config response, not the component props.

**Step 6: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds

**Step 7: Commit**

Message: `feat(dashboard): replace Sessions tab with model configuration editor`

---

### Task 6: Clean Up Removed Promptable Target References

Remove promptable target functions from ConfigPage that are no longer used. Check if any other tab or component depends on them.

**Files:**

- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`
- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts` (if promptable state is unused)

**Step 1: Search for promptable target usage outside SessionsTab**

Search the codebase for references to `promptableTargets`, `addPromptableTarget`, `removePromptableTarget`, `newPromptableName`, `newPromptableCommand` in files OTHER than SessionsTab.tsx. Key files to check:

- `ConfigPage.tsx` — function definitions (keep if used by other tabs like QuickLaunchTab)
- `useConfigForm.ts` — state fields (keep if used by save flow or hasChanges)
- `QuickLaunchTab.tsx` — may receive `promptableTargets` as props

If `QuickLaunchTab` or other components still use `promptableTargets` from state, keep the state field and reducer logic. Only remove the `addPromptableTarget`/`removePromptableTarget` callbacks from ConfigPage if nothing calls them.

**Step 2: Remove unused callbacks**

If `addPromptableTarget` and `removePromptableTarget` are only called from the old SessionsTab, remove those function definitions from ConfigPage.tsx.

**Step 3: Run all tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS

**Step 4: Run full test suite**

Run: `./test.sh --quick`
Expected: ALL PASS

**Step 5: Commit**

Message: `refactor(dashboard): remove unused promptable target callbacks from ConfigPage`

---

### Task 7: Full Regression

Run the full test suite to verify everything works end-to-end.

**Step 1: Run Go tests**

Run: `go test ./...`
Expected: ALL PASS

**Step 2: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: ALL PASS

**Step 3: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds

**Step 4: Run full suite**

Run: `./test.sh --quick`
Expected: ALL PASS

---

## Execution Notes

- Task 1 is backend-only (Go catalog expansion). Tasks 2-6 are frontend-only. They can be worked in parallel if desired.
- Task 3 is the core component. Task 4 adds the state management. Task 5 wires them together. This order lets you verify each piece in isolation.
- The `enabledModels` save path already works — line 573 of ConfigPage.tsx sends `state.enabledModels` as `enabled_models` in the update request. No save flow changes needed.
- The `hasChanges` detection already works — line 550 of useConfigForm.ts compares `enabledModels` via JSON.stringify. No change detection changes needed.
- The `ModelCatalog` component test file should be kept next to the component in `routes/config/`.
- Exact model version IDs (like `claude-opus-4-20250514` vs `claude-opus-4-20250115`) should be verified against Anthropic's actual model listing before committing Task 1. The plan uses plausible IDs but the implementer should check.
