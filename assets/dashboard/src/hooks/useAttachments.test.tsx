import { describe, it, expect, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useAttachments } from './useAttachments';

vi.mock('../lib/api', () => ({
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const png = (name: string) => new File(['p'], name, { type: 'image/png' });
const csv = (name: string) => new File(['c'], name, { type: 'text/csv' });

describe('useAttachments', () => {
  it('reads images inline with their media type and uploads other files', async () => {
    const upload = vi.fn(async (f: File) => ({ id: `id-${f.name}`, name: f.name }));
    const { result } = renderHook(() => useAttachments({ upload }));
    const jpeg = new File(['j'], 'photo.jpg', { type: 'image/jpeg' });

    await act(() => result.current.attachFiles([jpeg, csv('users.csv')]));

    expect(result.current.images).toEqual([{ media_type: 'image/jpeg', data: 'ag==' }]);
    expect(result.current.files).toEqual([{ id: 'id-users.csv', name: 'users.csv' }]);
    expect(upload).toHaveBeenCalledTimes(1);
    expect(result.current.error).toBeNull();
  });

  it('reports attaching until the upload settles', async () => {
    let finish!: (v: { id: string; name: string }) => void;
    const upload = vi.fn(() => new Promise<{ id: string; name: string }>((r) => (finish = r)));
    const { result } = renderHook(() => useAttachments({ upload }));

    let pending!: Promise<void>;
    act(() => {
      pending = result.current.attachFiles([csv('a.csv')]);
    });
    await waitFor(() => expect(result.current.attaching).toBe(true));
    await act(async () => {
      finish({ id: 'a', name: 'a.csv' });
      await pending;
    });
    expect(result.current.attaching).toBe(false);
  });

  it('reports a failed upload as filename: message and adds no file', async () => {
    const upload = vi.fn(async () => {
      throw new Error('file exceeds 50 MiB');
    });
    const { result } = renderHook(() => useAttachments({ upload }));
    await act(() => result.current.attachFiles([csv('big.csv')]));
    expect(result.current.files).toEqual([]);
    expect(result.current.error).toBe('big.csv: file exceeds 50 MiB');
  });

  it('caps images at maxImages and names the rejected file', async () => {
    const { result } = renderHook(() => useAttachments({ upload: vi.fn(), maxImages: 2 }));
    await act(() => result.current.attachFiles([png('1.png'), png('2.png'), png('3.png')]));
    expect(result.current.images).toHaveLength(2);
    expect(result.current.error).toBe('3.png: maximum 2 images');
  });

  it('ignores attach requests while disabled', async () => {
    const upload = vi.fn();
    const { result } = renderHook(() => useAttachments({ upload, disabled: true }));
    await act(() => result.current.attachFiles([csv('a.csv'), png('b.png')]));
    expect(upload).not.toHaveBeenCalled();
    expect(result.current.images).toEqual([]);
  });

  it('restores, removes, and clears', () => {
    const { result } = renderHook(() =>
      useAttachments<{ id: string; name: string }>({ upload: vi.fn() })
    );
    act(() =>
      result.current.restore(
        [{ media_type: 'image/png', data: 'AA==' }],
        [
          { id: '1', name: 'a.csv' },
          { id: '2', name: 'b.csv' },
        ]
      )
    );
    act(() => result.current.removeFile(0));
    expect(result.current.files).toEqual([{ id: '2', name: 'b.csv' }]);
    act(() => result.current.removeImage(0));
    expect(result.current.images).toEqual([]);
    act(() => result.current.clear());
    expect(result.current.files).toEqual([]);
  });
});
