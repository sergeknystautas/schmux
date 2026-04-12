import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import SessionsTab from './SessionsTab';
import { initialState } from './useConfigForm';
import type { ConfigFormAction } from './useConfigForm';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const defaultProps = {
  state: { ...initialState, loading: false },
  dispatch,
  models: [],
  personas: [],
  builtinQuickLaunch: [],
  onEditQuickLaunch: vi.fn(),
  onRemoveQuickLaunch: vi.fn(),
  onAddAgent: vi.fn(),
  onAddQuickLaunchCommand: vi.fn(),
  onAddFromCookbook: vi.fn(),
  onOpenPastebinEditModal: vi.fn(),
  onOpenAddPastebinModal: vi.fn(),
  onAddCommand: vi.fn(),
  onRemoveCommand: vi.fn(),
  onOpenRunTargetEditModal: vi.fn(),
};

describe('SessionsTab', () => {
  it('renders Quick Launch section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Quick Launch')).toBeInTheDocument();
  });

  it('renders Pastebin section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Pastebin')).toBeInTheDocument();
  });

  it('renders Command Targets section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Command Targets')).toBeInTheDocument();
  });

  it('renders NudgeNik section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('NudgeNik')).toBeInTheDocument();
  });

  it('renders Notifications section', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Notifications')).toBeInTheDocument();
  });

  it('renders Add Agent and Add Command buttons', () => {
    render(<SessionsTab {...defaultProps} />);
    expect(screen.getByText('Add Agent')).toBeInTheDocument();
    expect(screen.getByText('Add Command')).toBeInTheDocument();
  });
});
