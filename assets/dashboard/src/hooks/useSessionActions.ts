import { useCallback } from 'react';
import { disposeSession, getErrorMessage, updateNickname } from '../lib/api';
import { copyToClipboard } from '../lib/utils';
import { useModal } from '../components/ModalProvider';
import { useToast } from '../components/ToastProvider';
import type { SessionResponse } from '../lib/types';

/**
 * Session actions shared by the terminal and chat session pages: edit the
 * nickname, dispose the session, copy the attach command. Each wraps the
 * confirm/prompt modal and the toast the action needs.
 */
export function useSessionActions(
  sessionId: string | undefined,
  session: Pick<SessionResponse, 'nickname' | 'status' | 'attach_cmd'> | null | undefined
) {
  const { prompt, confirm, alert } = useModal();
  const { success, error: toastError } = useToast();

  const editNickname = useCallback(async () => {
    if (!sessionId || !session) return;
    let newNickname: string | null = session.nickname || '';
    let errorMessage = '';

    // Keep prompting until successful or cancelled
    while (true) {
      newNickname = await prompt('Edit Nickname', {
        defaultValue: newNickname,
        placeholder: 'Enter nickname (optional)',
        confirmText: 'Save',
        errorMessage,
      });

      if (newNickname === null) return; // User cancelled

      try {
        await updateNickname(sessionId, newNickname);
        success('Nickname updated');
        return;
      } catch (err) {
        if ((err as { isConflict?: boolean }).isConflict) {
          errorMessage = getErrorMessage(err, 'Nickname conflict');
        } else {
          alert(
            'Nickname Update Failed',
            `Failed to update nickname: ${getErrorMessage(err, 'Unknown error')}`
          );
          return;
        }
      }
    }
  }, [sessionId, session, prompt, success, alert]);

  const dispose = useCallback(async () => {
    if (!sessionId) return;
    if (session?.status === 'disposing') return;

    const sessionDisplay = session?.nickname ? `${session.nickname} (${sessionId})` : sessionId;

    const accepted = await confirm(`Dispose session ${sessionDisplay}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
    } catch (err) {
      alert('Dispose Failed', `Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
    }
  }, [sessionId, session?.nickname, session?.status, confirm, success, alert]);

  const copyAttach = useCallback(async () => {
    if (!session) return;
    const ok = await copyToClipboard(session.attach_cmd);
    if (ok) {
      success('Copied attach command');
    } else {
      toastError('Failed to copy');
    }
  }, [session, success, toastError]);

  return { editNickname, dispose, copyAttach };
}
