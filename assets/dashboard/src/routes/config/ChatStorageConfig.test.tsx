import React from 'react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import ChatStorageConfig from './ChatStorageConfig';
import type { ConfigFormAction } from './useConfigForm';
import * as api from '../../lib/api';

vi.mock('../../lib/api', () => ({
  getChatStorageStatus: vi.fn(),
}));

const noopDispatch: React.Dispatch<ConfigFormAction> = () => undefined;

describe('ChatStorageConfig', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders retention and budget values from props', () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 7,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });

    render(
      <ChatStorageConfig retentionDays={7} maxTotalStorageMB={4096} dispatch={noopDispatch} />
    );
    const retention = screen.getByTestId('chat-storage-retention') as HTMLInputElement;
    const budget = screen.getByTestId('chat-storage-budget') as HTMLInputElement;
    expect(retention.value).toBe('7');
    expect(budget.value).toBe('4096');
  });

  it('editing retention dispatches the updated value', async () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 7,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });
    const dispatched: Array<unknown> = [];
    const dispatch: React.Dispatch<ConfigFormAction> = (a) => {
      dispatched.push(a);
    };

    render(<ChatStorageConfig retentionDays={7} maxTotalStorageMB={4096} dispatch={dispatch} />);
    const retention = screen.getByTestId('chat-storage-retention') as HTMLInputElement;
    fireEvent.change(retention, { target: { value: '14' } });
    await waitFor(() => {
      expect(
        dispatched.find((a) => (a as { field: string }).field === 'chatStorageRetentionDays')
      ).toBeDefined();
    });
  });

  it('zero retention survives state and payload (does not fall back to default)', () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 0,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });

    render(
      <ChatStorageConfig retentionDays={0} maxTotalStorageMB={4096} dispatch={noopDispatch} />
    );
    const retention = screen.getByTestId('chat-storage-retention') as HTMLInputElement;
    expect(retention.value).toBe('0');
  });

  it('shows the error hint for negative retention', () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 0,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });

    render(
      <ChatStorageConfig retentionDays={-1} maxTotalStorageMB={4096} dispatch={noopDispatch} />
    );
    expect(screen.getByText(/whole number of days/)).toBeInTheDocument();
  });

  it('shows the error hint for budget below one', () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 7,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });

    render(<ChatStorageConfig retentionDays={7} maxTotalStorageMB={0} dispatch={noopDispatch} />);
    expect(screen.getByText(/whole number of MiB/)).toBeInTheDocument();
  });

  it('rejects fractional retention without dispatching a changed value', () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 7,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });
    const dispatched: Array<unknown> = [];
    const dispatch: React.Dispatch<ConfigFormAction> = (a) => dispatched.push(a);

    render(<ChatStorageConfig retentionDays={7} maxTotalStorageMB={4096} dispatch={dispatch} />);
    const retention = screen.getByTestId('chat-storage-retention') as HTMLInputElement;
    fireEvent.change(retention, { target: { value: '7.5' } });

    expect(retention.value).toBe('7.5');
    expect(screen.getByText(/whole number of days/)).toBeInTheDocument();
    expect(
      dispatched.find((a) => (a as { field: string }).field === 'chatStorageRetentionDays')
    ).toBeUndefined();
  });

  it('rejects empty budget without dispatching a changed value', () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 7,
      max_total_storage_mb: 4096,
      used_bytes: 0,
      active_bytes: 0,
      ended_bytes: 0,
      session_directories: 0,
      active_directories: 0,
    });
    const dispatched: Array<unknown> = [];
    const dispatch: React.Dispatch<ConfigFormAction> = (a) => dispatched.push(a);

    render(<ChatStorageConfig retentionDays={7} maxTotalStorageMB={4096} dispatch={dispatch} />);
    const budget = screen.getByTestId('chat-storage-budget') as HTMLInputElement;
    fireEvent.change(budget, { target: { value: '' } });

    expect(budget.value).toBe('');
    expect(screen.getByText(/whole number of MiB/)).toBeInTheDocument();
    expect(
      dispatched.find((a) => (a as { field: string }).field === 'chatStorageMaxTotalStorageMB')
    ).toBeUndefined();
  });

  it('renders status after a successful fetch', async () => {
    vi.mocked(api.getChatStorageStatus).mockResolvedValue({
      retention_days: 7,
      max_total_storage_mb: 4096,
      used_bytes: 1024,
      active_bytes: 0,
      ended_bytes: 1024,
      session_directories: 1,
      active_directories: 0,
      last_cleanup: {
        at: '2026-10-14T16:00:00Z',
        age_deleted: 5,
        budget_deleted: 0,
        bytes_reclaimed: 2048,
        errors: 0,
      },
    });

    render(
      <ChatStorageConfig retentionDays={7} maxTotalStorageMB={4096} dispatch={noopDispatch} />
    );
    await waitFor(() => {
      expect(screen.getByTestId('chat-storage-status').textContent).toMatch(/Current use/);
    });
    expect(screen.getByTestId('chat-storage-status').textContent).toMatch(/5 dirs/);
  });

  it('renders muted failure when status fetch rejects', async () => {
    vi.mocked(api.getChatStorageStatus).mockRejectedValue(new Error('boom'));

    render(
      <ChatStorageConfig retentionDays={7} maxTotalStorageMB={4096} dispatch={noopDispatch} />
    );
    await waitFor(() => {
      expect(screen.getByTestId('chat-storage-status').textContent).toMatch(/unavailable/);
    });
  });
});
