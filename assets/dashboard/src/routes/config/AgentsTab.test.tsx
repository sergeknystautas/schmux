import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import AgentsTab from './AgentsTab';
import type { ConfigFormAction } from './useConfigForm';
import type { Model } from '../../lib/types';

// Mock UserModelsEditor — it uses raw fetch internally
vi.mock('./UserModelsEditor', () => ({
  default: () => <div data-testid="user-models-editor">UserModelsEditor</div>,
}));

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const models: Model[] = [
  {
    id: 'claude-sonnet-4-6',
    display_name: 'Claude Sonnet 4.6',
    provider: 'anthropic',
    configured: true,
    runners: ['claude'],
  },
];

const defaultProps = {
  state: {
    commitMessageTarget: '',
    prReviewTarget: '',
    branchSuggestTarget: '',
    conflictResolveTarget: '',
    enabledModels: {} as Record<string, string>,
  },
  dispatch,
  models,
  oneshotModels: models,
  modelCatalog: models,
  runners: {} as Record<string, import('../../lib/types').RunnerInfo>,
  onModelAction: vi.fn(),
  onOpenRunTargetEditModal: vi.fn(),
  commitMessageTargetMissing: false,
  prReviewTargetMissing: false,
  branchSuggestTargetMissing: false,
  conflictResolveTargetMissing: false,
};

describe('AgentsTab', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders Task Assignments section heading', () => {
    render(<AgentsTab {...defaultProps} />);
    expect(screen.getByText('Task Assignments')).toBeInTheDocument();
  });

  it('renders Model Catalog section', () => {
    render(<AgentsTab {...defaultProps} />);
    expect(screen.getByText('Model Catalog')).toBeInTheDocument();
  });

  it('renders Task Assignments before Model Catalog in the DOM', () => {
    render(<AgentsTab {...defaultProps} />);
    const taskAssignments = screen.getByText('Task Assignments');
    const modelCatalog = screen.getByText('Model Catalog');

    // compareDocumentPosition returns a bitmask; bit 4 (DOCUMENT_POSITION_FOLLOWING)
    // means the other node follows this one in the document.
    const position = taskAssignments.compareDocumentPosition(modelCatalog);
    expect(position & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('renders all 4 target selectors', () => {
    render(<AgentsTab {...defaultProps} />);
    expect(screen.getByText('Commit Message')).toBeInTheDocument();
    expect(screen.getByText('PR Review')).toBeInTheDocument();
    expect(screen.getByText('Branch Suggestion')).toBeInTheDocument();
    expect(screen.getByText('Conflict Resolution')).toBeInTheDocument();
  });

  it('restricts task-assignment dropdowns by capability and shows full catalog in Model Catalog', () => {
    const alpha: Model = {
      id: 'alpha',
      display_name: 'Alpha',
      provider: 'anthropic',
      configured: true,
      runners: ['claude'],
    };
    const beta: Model = {
      id: 'beta',
      display_name: 'Beta',
      provider: 'google',
      configured: true,
      runners: ['gemini'],
    };
    const gamma: Model = {
      id: 'gamma',
      display_name: 'Gamma',
      provider: 'x',
      configured: false,
      runners: ['x'],
    };

    render(
      <AgentsTab
        {...defaultProps}
        models={[alpha, beta]}
        oneshotModels={[alpha]}
        modelCatalog={[alpha, beta, gamma]}
        runners={{
          claude: { available: true, capabilities: ['interactive', 'oneshot'] },
          gemini: { available: true, capabilities: ['interactive'] },
          x: { available: true, capabilities: ['interactive'] },
        }}
      />
    );

    const findSelect = (label: string) =>
      screen.getByText(label).closest('.form-group')?.querySelector('select') as HTMLSelectElement;

    const optionIds = (select: HTMLSelectElement) =>
      Array.from(select.options)
        .map((o) => o.value)
        .filter((v) => v !== '');

    // Oneshot-restricted dropdowns: only alpha.
    expect(optionIds(findSelect('Commit Message'))).toEqual(['alpha']);
    expect(optionIds(findSelect('Branch Suggestion'))).toEqual(['alpha']);
    expect(optionIds(findSelect('Conflict Resolution'))).toEqual(['alpha']);

    // PR Review uses enabled models: alpha and beta.
    expect(optionIds(findSelect('PR Review'))).toEqual(['alpha', 'beta']);

    // Model Catalog section shows the full catalog including gamma.
    expect(screen.getByText('Gamma')).toBeInTheDocument();
  });
});
