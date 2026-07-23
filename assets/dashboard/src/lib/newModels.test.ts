import { describe, it, expect } from 'vitest';
import { selectNewModels } from './newModels';
import type { Model } from './types';

const base = (over: Partial<Model>): Model => ({
  id: 'x',
  display_name: 'X',
  provider: 'p',
  configured: true,
  runners: ['claude'],
  ...over,
});

const now = new Date('2026-07-18T00:00:00Z');

describe('selectNewModels', () => {
  it('keeps only configured models released within the window, newest first', () => {
    const models = [
      base({ id: 'new-keyed', configured: true, release_date: '2026-07-16' }),
      base({ id: 'new-unconfigured', configured: false, release_date: '2026-07-17' }),
      base({ id: 'old', configured: true, release_date: '2026-01-01' }),
      base({ id: 'newer', configured: true, release_date: '2026-07-17' }),
    ];
    const out = selectNewModels(models, now, 30, 5).map((m) => m.id);
    expect(out).toEqual(['newer', 'new-keyed']);
  });

  it('excludes absent/unparseable release dates and caps the count', () => {
    const models = [
      base({ id: 'nodate', configured: true }),
      base({ id: 'bad', configured: true, release_date: 'not-a-date' }),
      ...Array.from({ length: 8 }, (_, i) =>
        base({ id: `m${i}`, configured: true, release_date: '2026-07-10' })
      ),
    ];
    const out = selectNewModels(models, now, 30, 5);
    expect(out).toHaveLength(5);
    expect(out.find((m) => m.id === 'nodate' || m.id === 'bad')).toBeUndefined();
  });
});
