import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
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
    expect(dropsEl.className).toContain('warning');
  });
});
