import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import FlightMonitorHistory from './FlightMonitorHistory';
import { retryFlightMonitor } from '../../lib/api';

vi.mock('../../lib/api', () => ({ retryFlightMonitor: vi.fn() }));

describe('FlightMonitorHistory', () => {
  it('shows the retained questions and response, and retries a failed classification', async () => {
    vi.mocked(retryFlightMonitor).mockResolvedValue(
      {} as Awaited<ReturnType<typeof retryFlightMonitor>>
    );
    render(
      <FlightMonitorHistory
        flightID="f"
        sessions={[]}
        monitor={{
          turns: [
            {
              id: 'turn-planner-2',
              session_id: 'planner',
              start_seq: 1,
              end_seq: 2,
              text: 'Planner response',
              outcome: 'completed',
              status: 'failed',
              created_at: '2026-10-18T00:00:00Z',
              updated_at: '2026-10-18T00:00:00Z',
              attempts: [
                {
                  error: 'API unavailable',
                  created_at: '2026-10-18T00:00:00Z',
                  questions: [
                    {
                      id: 'attention',
                      question: 'Saved question before YAML edit',
                      choices: [{ id: 'routine', description: 'Saved choice description' }],
                      notify_on: ['routine'],
                      min_confidence: 0.7,
                    },
                  ],
                },
              ],
            },
          ],
        }}
      />
    );
    await userEvent.click(screen.getByRole('button', { name: 'Show response' }));
    expect(screen.getByText('Planner response')).toBeInTheDocument();
    expect(screen.getByText(/Saved question before YAML edit/)).toBeInTheDocument();
    expect(screen.getByText(/Saved choice description/)).toBeInTheDocument();
    expect(screen.getByText(/API unavailable/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(retryFlightMonitor).toHaveBeenCalledExactlyOnceWith('f', 'turn-planner-2');
  });

  it('shows each reported spec and plan with its check', async () => {
    const turn = {
      session_id: 'planner',
      start_seq: 1,
      end_seq: 2,
      text: 'Spec: /w/s.md\nPlan: /w/.schmux/p.md',
      outcome: 'completed',
      status: 'classified',
      created_at: '2026-10-18T00:00:00Z',
      updated_at: '2026-10-18T00:00:00Z',
    };
    render(
      <FlightMonitorHistory
        flightID="f"
        sessions={[]}
        monitor={{
          turns: [
            {
              ...turn,
              id: 'turn-bad',
              artifacts: {
                spec_path: '/w/s.md',
                plan_path: '/w/.schmux/p.md',
                error: 'plan /w/.schmux/p.md is ignored by git, so it cannot be committed',
              },
            },
            { ...turn, id: 'turn-good', artifacts: { spec_path: '/w/s.md', plan_path: '/w/p.md' } },
          ],
        }}
      />
    );
    const pills = screen.getAllByTestId('flight-monitor-turn-artifacts');
    expect(pills.map((p) => p.textContent)).toEqual([
      'spec/plan check failed',
      'spec/plan verified',
    ]);
    await userEvent.click(screen.getAllByRole('button', { name: 'Show response' })[0]);
    expect(screen.getByTestId('flight-monitor-turn-report')).toHaveTextContent(
      'Plan: /w/.schmux/p.md'
    );
    expect(screen.getByText(/is ignored by git, so it cannot be committed/)).toBeInTheDocument();
  });

  it('shows an executor turn with its report check and skill evidence', async () => {
    const turn = {
      session_id: 'executor',
      start_seq: 1,
      end_seq: 2,
      text: 'Implemented the plan.',
      outcome: 'completed',
      status: 'classified',
      created_at: '2026-10-18T00:00:00Z',
      updated_at: '2026-10-18T00:00:00Z',
    };
    const skill = 'executing-plans invoked at record 21';
    render(
      <FlightMonitorHistory
        flightID="f"
        sessions={[]}
        monitor={{
          turns: [
            {
              ...turn,
              id: 'turn-pending',
              execution: {
                skill,
                report_path: '/w/p-execution.md',
                report_verified: false,
                report_error: 'not written yet',
              },
            },
            {
              ...turn,
              id: 'turn-empty',
              execution: {
                skill: 'executing-plans not found',
                report_path: '/w/p-execution.md',
                report_verified: false,
                report_error: '/w/p-execution.md is empty',
              },
            },
            {
              ...turn,
              id: 'turn-done',
              execution: { skill, report_path: '/w/p-execution.md', report_verified: true },
            },
          ],
        }}
      />
    );

    const pills = screen.getAllByTestId('flight-monitor-turn-execution');
    expect(pills.map((p) => p.textContent)).toEqual([
      'report not written',
      'report check failed',
      'report verified',
    ]);
    await userEvent.click(screen.getAllByRole('button', { name: 'Show response' })[0]);
    expect(screen.getByTestId('flight-monitor-turn-execution-detail')).toHaveTextContent(
      `Skill check: ${skill}`
    );
    expect(screen.getByTestId('flight-monitor-turn-execution-detail')).toHaveTextContent(
      'Report check: not written yet'
    );
  });

  it('names the role each message was sent to, including a replaced session', () => {
    const at = '2026-10-18T00:00:00Z';
    const assignment = (
      role: 'pilot' | 'planner' | 'executor',
      sessionId: string,
      prior: string[] = []
    ) => ({
      id: `sa-${role}`,
      role,
      target: 'm',
      session_id: sessionId,
      prior_session_ids: prior,
      outcome: 'succeeded',
      available: true,
      created_at: at,
      updated_at: at,
    });
    const message = (id: string, kind: string, recipient: string) => ({
      id,
      kind,
      recipient_session_id: recipient,
      text: `text of ${id}`,
      status: 'accepted',
      created_at: at,
      updated_at: at,
    });
    render(
      <FlightMonitorHistory
        flightID="f"
        sessions={[
          assignment('pilot', 'pilot-1'),
          assignment('planner', 'planner-1'),
          assignment('executor', 'executor-2', ['executor-1']),
        ]}
        monitor={{
          messages: [
            message('m-exec', 'pilot_message', 'executor-1'),
            message('m-plan', 'pilot_message', 'planner-1'),
            message('m-note', 'notification', 'pilot-1'),
            message('m-start', 'execution_notice', 'pilot-1'),
            message('m-reject', 'rejection', 'pilot-1'),
          ],
        }}
      />
    );

    expect(
      screen.getAllByTestId('flight-monitor-message-recipient').map((e) => e.textContent)
    ).toEqual([
      'Pilot message to Executor',
      'Pilot message to Planner',
      'Notification to Pilot',
      'Execution notice to Pilot',
      'Rejected decision to Pilot',
    ]);
  });

  it('shows delivery errors and surfaces a failed retry', async () => {
    vi.mocked(retryFlightMonitor).mockRejectedValue(new Error('Retry unavailable'));
    render(
      <FlightMonitorHistory
        flightID="f"
        sessions={[]}
        monitor={{
          messages: [
            {
              id: 'm',
              kind: 'notification',
              recipient_session_id: 'pilot',
              text: 'Combined notification',
              status: 'failed',
              error: 'Pilot unavailable',
              created_at: '2026-10-18T00:00:00Z',
              updated_at: '2026-10-18T00:00:00Z',
            },
          ],
        }}
      />
    );
    expect(screen.getByText('Combined notification')).toBeInTheDocument();
    expect(screen.getByText('Pilot unavailable')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Retry unavailable');
  });
});
