import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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
});
