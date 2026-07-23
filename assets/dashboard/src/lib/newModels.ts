import type { Model } from './types';

// selectNewModels returns configured models released within `windowDays`,
// newest first, capped at `limit`. Absent/unparseable release dates are excluded.
export function selectNewModels(models: Model[], now: Date, windowDays = 30, limit = 5): Model[] {
  const cutoff = now.getTime() - windowDays * 86400000;
  return models
    .filter((m) => m.configured)
    .map((m) => ({ m, t: m.release_date ? new Date(m.release_date).getTime() : NaN }))
    .filter(({ t }) => !Number.isNaN(t) && t >= cutoff && t <= now.getTime())
    .sort((a, b) => b.t - a.t)
    .slice(0, limit)
    .map(({ m }) => m);
}
