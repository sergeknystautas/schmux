import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useFloorManager } from './useFloorManager';
import type { SessionWithWorkspace } from '../lib/types';

// --- Mocks ---

let mockSessionsById: Record<string, SessionWithWorkspace> = {};
let mockSessionsLoading = false;

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    sessionsById: mockSessionsById,
    loading: mockSessionsLoading,
  }),
}));

let mockConfig: Record<string, unknown> = {};
let mockConfigLoading = false;

vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: mockConfig,
    loading: mockConfigLoading,
  }),
}));

function makeSession(overrides: Partial<SessionWithWorkspace> = {}): SessionWithWorkspace {
  return {
    id: 'sess-1',
    target: 'claude',
    branch: 'main',
    created_at: new Date().toISOString(),
    running: true,
    attach_cmd: 'tmux attach -t test',
    workspace_id: 'ws-1',
    workspace_path: '/tmp/ws',
    repo: 'test-repo',
    ...overrides,
  };
}

describe('useFloorManager', () => {
  beforeEach(() => {
    mockSessionsById = {};
    mockSessionsLoading = false;
    mockConfig = {};
    mockConfigLoading = false;
  });

  it('returns session ID when floor manager session exists', () => {
    mockConfig = { floor_manager: { enabled: true } };
    mockSessionsById = {
      'fm-123': makeSession({
        id: 'fm-123',
        is_floor_manager: true,
        running: true,
      }),
    };

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.enabled).toBe(true);
    expect(result.current.sessionId).toBe('fm-123');
    expect(result.current.running).toBe(true);
    expect(result.current.loading).toBe(false);
  });

  it('returns null session ID when no floor manager session exists', () => {
    mockConfig = { floor_manager: { enabled: false } };
    mockSessionsById = {
      'sess-1': makeSession({ id: 'sess-1' }),
    };

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.enabled).toBe(false);
    expect(result.current.sessionId).toBeNull();
    expect(result.current.running).toBe(false);
  });

  it('returns loading true while sessions or config are loading', () => {
    mockSessionsLoading = true;
    mockConfigLoading = false;

    const { result } = renderHook(() => useFloorManager());
    expect(result.current.loading).toBe(true);
  });

  it('detects running state correctly when floor manager is stopped', () => {
    mockConfig = { floor_manager: { enabled: true } };
    mockSessionsById = {
      'fm-456': makeSession({
        id: 'fm-456',
        is_floor_manager: true,
        running: false,
      }),
    };

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.enabled).toBe(true);
    expect(result.current.sessionId).toBe('fm-456');
    expect(result.current.running).toBe(false);
  });

  it('ignores non-floor-manager sessions', () => {
    mockConfig = { floor_manager: { enabled: true } };
    mockSessionsById = {
      'sess-1': makeSession({ id: 'sess-1', running: true }),
      'sess-2': makeSession({ id: 'sess-2', running: true }),
    };

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.sessionId).toBeNull();
    expect(result.current.running).toBe(false);
  });

  it('defaults enabled to false when config has no floor_manager', () => {
    mockConfig = {};

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.enabled).toBe(false);
  });

  it('returns escalation from floor manager session', () => {
    mockConfig = { floor_manager: { enabled: true } };
    mockSessionsById = {
      'fm-789': makeSession({
        id: 'fm-789',
        is_floor_manager: true,
        running: true,
        escalation: 'Agent needs help with database migration',
      }),
    };

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.escalation).toBe('Agent needs help with database migration');
  });

  it('returns undefined escalation when no escalation is set', () => {
    mockConfig = { floor_manager: { enabled: true } };
    mockSessionsById = {
      'fm-123': makeSession({
        id: 'fm-123',
        is_floor_manager: true,
        running: true,
      }),
    };

    const { result } = renderHook(() => useFloorManager());

    expect(result.current.escalation).toBeUndefined();
  });
});
