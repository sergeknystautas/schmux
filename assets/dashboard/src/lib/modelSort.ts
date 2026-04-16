import type { Model } from './types';

const TIER_ORDER: Record<string, number> = { haiku: 0, sonnet: 1, opus: 2, flash: 0, pro: 1 };

function modelSortKey(model: Model): [number, number] {
  const name = model.display_name.toLowerCase();
  const parts = name.split(/\s+/);
  let tier = 99;
  let version = 0;
  for (const part of parts) {
    if (part in TIER_ORDER) tier = TIER_ORDER[part];
    const v = parseFloat(part);
    if (!isNaN(v) && v > 0) version = v;
  }
  return [tier, version];
}

export function sortModels(models: Model[]): Model[] {
  return [...models].sort((a, b) => {
    const [aTier, aVer] = modelSortKey(a);
    const [bTier, bVer] = modelSortKey(b);
    if (aTier !== bTier) return aTier - bTier;
    return aVer - bVer;
  });
}
