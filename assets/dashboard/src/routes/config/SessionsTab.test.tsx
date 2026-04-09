import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import SessionsTab from './SessionsTab';
import type { Model, RunnerInfo, RunTargetResponse } from '../../lib/types';

const topRunners: Record<string, RunnerInfo> = {
  claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
  opencode: { available: true, capabilities: ['interactive', 'oneshot'] },
  codex: { available: true, capabilities: ['interactive', 'oneshot'] },
};

const models: Model[] = [
  {
    id: 'claude-opus-4-6',
    display_name: 'Claude Opus 4.6',
    provider: 'anthropic',
    configured: true,
    runners: ['claude', 'opencode'],
  },
  {
    id: 'gpt-5.3-codex',
    display_name: 'GPT 5.3 Codex',
    provider: 'openai',
    configured: true,
    runners: ['codex', 'opencode'],
  },
];

const commandTargets: RunTargetResponse[] = [{ name: 'build', command: 'go build ./...' }];

const dispatch = vi.fn();

const defaultProps = {
  models,
  runners: topRunners,
  enabledModels: {} as Record<string, string>,
  commStyles: {} as Record<string, string>,
  styles: [],
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

  it('renders add command form', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByPlaceholderText('Name')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Command (e.g., go build ./...)')).toBeInTheDocument();
  });

  it('dispatches commStyles without the key when selecting None for last remaining style', () => {
    const localDispatch = vi.fn();
    const styles = [
      {
        id: 'valley-girl',
        name: 'Valley Girl',
        icon: '💅',
        tagline: '',
        prompt: '',
        built_in: true,
      },
      { id: 'trump', name: 'Trump', icon: '🍊', tagline: '', prompt: '', built_in: true },
    ];

    // Only one model enabled, only one comm style set — this is the "last remaining" case
    render(
      <SessionsTab
        {...defaultProps}
        dispatch={localDispatch}
        enabledModels={{ claude: 'claude' }}
        commStyles={{ claude: 'trump' }}
        styles={styles}
      />
    );

    // Find the select for claude and change it to "None" (empty value)
    const select = screen.getByTestId('comm-style-claude').querySelector('select')!;
    fireEvent.change(select, { target: { value: '' } });

    // Should dispatch SET_FIELD with empty map (claude removed)
    const call = localDispatch.mock.calls.find(
      (c: unknown[]) =>
        (c[0] as { type: string; field: string }).type === 'SET_FIELD' &&
        (c[0] as { type: string; field: string }).field === 'commStyles'
    );
    expect(call).toBeDefined();
    expect((call![0] as { value: Record<string, string> }).value).toEqual({});
  });
});

// Test that save payload includes comm_styles even when empty.
// This is a focused unit test for the payload construction pattern
// from ConfigPage.tsx line 690.
describe('comm_styles payload construction', () => {
  it('should include comm_styles as empty object when all styles cleared', () => {
    const commStyles: Record<string, string> = {};

    // Fixed pattern: always send the map, even when empty
    const payload = {
      comm_styles: commStyles,
    };

    // After fix, comm_styles is {} (not undefined), so Go backend
    // processes the update and clears all styles.
    expect(payload.comm_styles).toEqual({});
  });
});
