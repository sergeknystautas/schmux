import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import FlightStory from './FlightStory';
import { retryFlightMonitor } from '../../lib/api';
import type { FlightDetailResponse } from '../../lib/types.generated';

vi.mock('../../lib/api', () => ({ retryFlightMonitor: vi.fn() }));

const T = '2026-10-27T19:00:00-07:00';

const flight: Pick<FlightDetailResponse, 'id' | 'sessions' | 'operations' | 'monitor'> = {
  id: 'fl-1',
  sessions: [
    {
      id: 'sa-pilot',
      role: 'pilot',
      target: 'claude',
      session_id: 'pilot-1',
      outcome: 'succeeded',
      available: true,
      created_at: T,
      updated_at: T,
    },
    {
      id: 'sa-reviewer',
      role: 'reviewer',
      target: 'claude',
      session_id: 'reviewer-2',
      prior_session_ids: ['reviewer-1'],
      outcome: 'succeeded',
      available: true,
      created_at: T,
      updated_at: T,
    },
  ],
  operations: [
    {
      id: 'op-1',
      action: 'review',
      correlation_id: 'c',
      status: 'succeeded',
      created_at: '2026-10-27T19:01:00-07:00',
      updated_at: T,
      resources: [{ kind: 'session', id: 'reviewer-1', status: 'succeeded' }],
    },
  ],
  monitor: {
    turns: [
      {
        id: 'turn-reviewer-1-9',
        session_id: 'reviewer-1',
        start_seq: 3,
        end_seq: 9,
        text: 'I changed the parser test and re-ran the suite.',
        outcome: 'completed',
        status: 'failed',
        created_at: '2026-10-27T19:02:00-07:00',
        updated_at: T,
        review: { skill: 'test-rules-review not found' },
        attempts: [{ created_at: T, error: 'API unavailable', questions: [], answers: [] }],
      },
    ],
    messages: [
      {
        id: 'msg-1',
        kind: 'pilot_message',
        sender_session_id: 'pilot-1',
        recipient_session_id: 'reviewer-2',
        text: 'Re-check the parser.',
        status: 'accepted',
        chat_message_id: 'chat-1',
        created_at: '2026-10-27T19:03:00-07:00',
        updated_at: T,
      },
    ],
  },
};

describe('FlightStory', () => {
  it('tells the flight newest first, with identifiers and texts behind Details', async () => {
    render(<FlightStory flight={flight} />);

    const headlines = screen.getAllByTestId('flight-story-headline').map((e) => e.textContent);
    expect(headlines).toEqual([
      'Pilot → Reviewer 2: Re-check the parser.',
      'Reviewer 1 responded; classification failed: API unavailable',
      'Entered review: reviewer started',
    ]);
    expect(screen.getByTestId('flight-story-tag')).toHaveTextContent('test-rules-review not found');
    expect(screen.queryByTestId('flight-story-detail')).not.toBeInTheDocument();
    expect(screen.queryByText('chat-1')).not.toBeInTheDocument();

    const entries = screen.getAllByTestId('flight-story-entry');
    await userEvent.click(within(entries[0]).getByRole('button', { name: 'Details' }));
    const detail = within(entries[0]).getByTestId('flight-story-detail');
    expect(detail).toHaveTextContent('chat-1');
    expect(detail).toHaveTextContent('Reviewer 2 (reviewer-2)');
    expect(within(detail).getByTestId('flight-story-text')).toHaveTextContent(
      'Re-check the parser.'
    );

    await userEvent.click(within(entries[1]).getByRole('button', { name: 'Details' }));
    const turnDetail = within(entries[1]).getByTestId('flight-story-detail');
    expect(turnDetail).toHaveTextContent('turn-reviewer-1-9');
    expect(turnDetail).toHaveTextContent('3–9');
    expect(turnDetail).toHaveTextContent('I changed the parser test and re-ran the suite.');
  });

  it('retries a failed item from its entry and surfaces a failed retry', async () => {
    vi.mocked(retryFlightMonitor).mockRejectedValueOnce(new Error('turn is classified'));
    render(<FlightStory flight={flight} />);

    const retries = screen.getAllByTestId('flight-story-retry');
    expect(retries).toHaveLength(1);
    await userEvent.click(retries[0]);

    expect(retryFlightMonitor).toHaveBeenCalledExactlyOnceWith('fl-1', 'turn-reviewer-1-9');
    expect(await screen.findByRole('alert')).toHaveTextContent('turn is classified');
  });

  it('shows the monitor error above the story and says when nothing has happened', () => {
    const { rerender } = render(
      <FlightStory flight={{ id: 'fl-1', monitor: { error: 'monitor: session x: not found' } }} />
    );
    expect(screen.getByTestId('flight-monitor-error')).toHaveTextContent('session x: not found');

    rerender(<FlightStory flight={{ id: 'fl-1' }} />);
    expect(screen.getByText('Nothing has happened yet.')).toBeInTheDocument();
  });
});
