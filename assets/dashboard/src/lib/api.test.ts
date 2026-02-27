import { describe, it, expect } from 'vitest';
import { getErrorMessage, getWorkspaceFileUrl, LinearSyncError } from './api';

describe('getErrorMessage', () => {
  it('extracts message from Error instance', () => {
    expect(getErrorMessage(new Error('test error'), 'fallback')).toBe('test error');
  });

  it('returns string directly', () => {
    expect(getErrorMessage('string error', 'fallback')).toBe('string error');
  });

  it('returns fallback for object', () => {
    expect(getErrorMessage({ some: 'object' }, 'fallback')).toBe('fallback');
  });

  it('returns fallback for number', () => {
    expect(getErrorMessage(42, 'fallback')).toBe('fallback');
  });

  it('returns fallback for null', () => {
    expect(getErrorMessage(null, 'fallback')).toBe('fallback');
  });

  it('returns fallback for undefined', () => {
    expect(getErrorMessage(undefined, 'fallback')).toBe('fallback');
  });
});

describe('LinearSyncError', () => {
  it('is an instance of Error', () => {
    const err = new LinearSyncError('sync failed', false);
    expect(err).toBeInstanceOf(Error);
  });

  it('has name "LinearSyncError"', () => {
    const err = new LinearSyncError('sync failed', false);
    expect(err.name).toBe('LinearSyncError');
  });

  it('preserves message', () => {
    const err = new LinearSyncError('sync failed', false);
    expect(err.message).toBe('sync failed');
  });

  it('preserves isPreCommitHookError flag', () => {
    const err = new LinearSyncError('hook failed', true, 'lint errors');
    expect(err.isPreCommitHookError).toBe(true);
  });

  it('preserves preCommitErrorDetail', () => {
    const err = new LinearSyncError('hook failed', true, 'lint errors');
    expect(err.preCommitErrorDetail).toBe('lint errors');
  });

  it('preCommitErrorDetail is undefined when not provided', () => {
    const err = new LinearSyncError('sync failed', false);
    expect(err.preCommitErrorDetail).toBeUndefined();
  });
});

describe('getWorkspaceFileUrl', () => {
  it('encodes the file path', () => {
    expect(getWorkspaceFileUrl('ws-1', 'src/foo.ts')).toBe('/api/file/ws-1/src%2Ffoo.ts');
  });

  it('handles nested paths with special characters', () => {
    expect(getWorkspaceFileUrl('ws-1', 'path/to/file with spaces.ts')).toBe(
      '/api/file/ws-1/path%2Fto%2Ffile%20with%20spaces.ts'
    );
  });

  it('handles root-level files', () => {
    expect(getWorkspaceFileUrl('ws-1', 'README.md')).toBe('/api/file/ws-1/README.md');
  });

  it('handles empty file path', () => {
    expect(getWorkspaceFileUrl('ws-1', '')).toBe('/api/file/ws-1/');
  });
});
