import { expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import AssistantTurnView from './AssistantTurnView';
import { reduceRecords } from '../../lib/chat/reducer';
import type { AssistantTurn, HarnessLine } from '../../lib/chat/types';

vi.mock('../ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

it('explains a Codex writer conflict and discloses the original error on demand', async () => {
  const raw = 'thread 01a0ccb6-c820-7401-a682-316f292c51da already has an active writer';
  const conversation = reduceRecords('codex-app-server', [
    {
      ts: '2026-11-01T17:51:41Z',
      type: 'harness',
      line: { id: 3, error: { code: -32600, message: raw } } as unknown as HarnessLine,
    },
  ]);
  render(
    <AssistantTurnView
      turn={conversation.items[0] as AssistantTurn}
      onPermission={vi.fn()}
      onAnswer={vi.fn()}
      onAbort={vi.fn()}
    />
  );

  expect(screen.getByRole('alert')).toHaveTextContent('Codex couldn’t resume this conversation.');
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Messages will remain queued until that process releases the conversation.'
  );
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Restarting this session alone may not resolve it.'
  );
  expect(screen.getByText(raw)).not.toBeVisible();

  await userEvent.click(screen.getByText('Technical details'));
  expect(screen.getByText(raw)).toBeVisible();
});
