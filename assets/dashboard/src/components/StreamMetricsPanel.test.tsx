import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { StreamMetricsPanel } from './StreamMetricsPanel';

describe('StreamMetricsPanel', () => {
  it('renders summary line with stats', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 1234,
          eventsDropped: 0,
          bytesDelivered: 867000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 1200,
          bytesReceived: 850000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
        }}
      />
    );
    expect(screen.getByText(/1\.2K frames/)).toBeTruthy();
    expect(screen.getByText(/0 drops/)).toBeTruthy();
    expect(screen.getByText(/0 seq breaks/)).toBeTruthy();
  });

  it('highlights non-zero drops in red', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 5,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 95,
          bytesReceived: 48000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
        }}
      />
    );
    const dropsEl = screen.getByText(/5 drops/);
    expect(dropsEl.getAttribute('data-severity')).toBe('warning');
  });

  it('shows break details when expanded and toggled', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 10,
          bytesReceived: 5000,
          bootstrapCount: 1,
          sequenceBreaks: 2,
          recentBreaks: [
            { frameIndex: 3, byteOffset: 1500, tail: '1b 5b' },
            { frameIndex: 7, byteOffset: 3200, tail: '1b 5b 33' },
          ],
        }}
      />
    );

    // Expand the dropdown by clicking the pill
    fireEvent.click(screen.getByText(/2 seq breaks/));

    // Break details should be collapsed by default
    expect(screen.queryByTestId('break-details-table')).toBeNull();

    // Toggle to show details
    fireEvent.click(screen.getByTestId('toggle-break-details'));
    expect(screen.getByTestId('break-details-table')).toBeTruthy();

    // Verify break data is rendered
    expect(screen.getByText('3')).toBeTruthy();
    expect(screen.getByText('1b 5b')).toBeTruthy();
    expect(screen.getByText('1b 5b 33')).toBeTruthy();
  });

  it('does not show break details toggle when sequenceBreaks is 0', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 10,
          bytesReceived: 5000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
          recentBreaks: [],
        }}
      />
    );

    fireEvent.click(screen.getByText(/0 seq breaks/));
    expect(screen.queryByTestId('toggle-break-details')).toBeNull();
  });

  it('does not show break details toggle when recentBreaks is undefined', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 10,
          bytesReceived: 5000,
          bootstrapCount: 1,
          sequenceBreaks: 3,
        }}
      />
    );

    fireEvent.click(screen.getByText(/3 seq breaks/));
    expect(screen.queryByTestId('toggle-break-details')).toBeNull();
  });

  it('hides break details on second toggle click', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 10,
          bytesReceived: 5000,
          bootstrapCount: 1,
          sequenceBreaks: 1,
          recentBreaks: [{ frameIndex: 1, byteOffset: 500, tail: '1b' }],
        }}
      />
    );

    fireEvent.click(screen.getByText(/1 seq breaks/));
    fireEvent.click(screen.getByTestId('toggle-break-details'));
    expect(screen.getByTestId('break-details-table')).toBeTruthy();

    // Click again to hide
    fireEvent.click(screen.getByTestId('toggle-break-details'));
    expect(screen.queryByTestId('break-details-table')).toBeNull();
  });

  it('formats byte offset in break details table', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 10,
          bytesReceived: 5000,
          bootstrapCount: 1,
          sequenceBreaks: 2,
          recentBreaks: [
            { frameIndex: 1, byteOffset: 500, tail: '1b' },
            { frameIndex: 5, byteOffset: 1500000, tail: '1b 5b' },
          ],
        }}
      />
    );

    fireEvent.click(screen.getByText(/2 seq breaks/));
    fireEvent.click(screen.getByTestId('toggle-break-details'));

    // 500B should render as "500B"
    expect(screen.getByText('500B')).toBeTruthy();
    // 1500000B should render as "1.5MB"
    expect(screen.getByText('1.5MB')).toBeTruthy();
  });

  it('renders frame size histogram when stats present and panel expanded', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 50,
          bytesReceived: 25000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
          frameSizeStats: { count: 50, median: 256, p90: 1024, max: 4096 },
          frameSizeDist: {
            buckets: [10, 8, 6, 4, 3, 2, 2, 1, 1, 1],
            maxCount: 10,
            maxBytes: 1024,
          },
        }}
      />
    );

    // Expand the dropdown
    fireEvent.click(screen.getByText(/50 frames/));

    // Frame size stats row should not be a separate text row — only histogram
    expect(screen.queryByText(/Frame sizes/)).toBeNull();

    // Histogram should render
    expect(screen.getByTestId('frame-size-histogram')).toBeTruthy();
  });

  it('does not render frame size histogram when stats are absent', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 10,
          bytesReceived: 5000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
        }}
      />
    );

    fireEvent.click(screen.getByText(/10 frames/));
    expect(screen.queryByTestId('frame-size-histogram')).toBeNull();
  });
});
