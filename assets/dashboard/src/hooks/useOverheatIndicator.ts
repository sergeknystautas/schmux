import { useEffect, useState } from 'react';

const SAMPLE_INTERVAL_MS = 500;
const LAG_THRESHOLD_MS = 150;
const HOT_STREAK_THRESHOLD = 5;
const HEALTHY_STREAK_TO_CLEAR = 10;

export default function useOverheatIndicator() {
  const [overheating, setOverheating] = useState(false);

  useEffect(() => {
    let hotStreak = 0;
    let healthyStreak = 0;
    let expected = Date.now() + SAMPLE_INTERVAL_MS;

    const id = window.setInterval(() => {
      const now = Date.now();
      const lag = now - expected;
      expected = now + SAMPLE_INTERVAL_MS;

      if (lag > LAG_THRESHOLD_MS) {
        hotStreak += 1;
        healthyStreak = 0;
        if (hotStreak >= HOT_STREAK_THRESHOLD) {
          setOverheating(true);
        }
        return;
      }

      hotStreak = 0;
      if (overheating) {
        healthyStreak += 1;
        if (healthyStreak >= HEALTHY_STREAK_TO_CLEAR) {
          setOverheating(false);
          healthyStreak = 0;
        }
      }
    }, SAMPLE_INTERVAL_MS);

    return () => window.clearInterval(id);
  }, [overheating]);

  return overheating;
}
