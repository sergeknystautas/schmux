import { csrfHeaders } from '../csrf';

// These keys are read only to recover samples captured by older dashboard code.
const LEGACY_LOADS_KEY = 'schmux:chat-load-samples';
const LEGACY_IMAGES_KEY = 'schmux:chat-image-load-samples';
const LEGACY_PENDING_LOADS_KEY = 'schmux:chat-load-pending';
const LEGACY_PENDING_IMAGES_KEY = 'schmux:chat-image-load-pending';
let navigationStart: { sessionId: string; at: number } | null = null;
let migratingLegacy = false;

export function markSessionNavigation(sessionId: string): void {
  navigationStart = { sessionId, at: performance.now() };
}

export function getSessionNavigation(sessionId: string): number | undefined {
  const start = navigationStart;
  if (!start || start.sessionId !== sessionId) return undefined;
  return performance.now() - start.at < 30_000 ? start.at : undefined;
}

export function clearSessionNavigation(sessionId: string): void {
  if (navigationStart?.sessionId === sessionId) navigationStart = null;
}

export interface ChatLoadSample {
  sessionId: string;
  loadId?: string;
  at: string;
  start: 'click' | 'view' | 'reconnect';
  frameChars: number;
  records: number;
  routeToSocketMs: number;
  socketOpenMs: number;
  historyWaitMs: number;
  parseMs: number;
  resolveMs?: number;
  reduceMs: number;
  commitMs: number;
  afterPaintMs: number;
  totalMs: number;
  reduction?: ChatReductionProfile;
}

export interface ChatReductionProfile {
  categories: { category: string; records: number; durationMs: number; maxMs: number }[];
  probeOverheadMs: number;
  items: number;
  turns: number;
  segments: number;
  images: number;
  operations: number;
}

export function captureChatLoad(sample: ChatLoadSample): void {
  void upload([sample], []).then((sent) => {
    if (sent) void migrateLegacySamples();
  });
}

export function captureChatImageLoad(img: HTMLImageElement, detailed = false): void {
  const entries = performance.getEntriesByName(img.currentSrc, 'resource');
  const entry = entries[entries.length - 1] as PerformanceResourceTiming | undefined;
  const sample = {
    path: new URL(img.currentSrc, window.location.href).pathname,
    at: new Date().toISOString(),
    resourceMs: entry?.duration ?? null,
    loadMs: entry ? performance.now() - entry.startTime : null,
    transferBytes: entry?.transferSize ?? null,
    decodedBytes: entry?.decodedBodySize ?? null,
    width: img.naturalWidth,
    height: img.naturalHeight,
    ...(detailed && entry
      ? {
          ttfbMs: entry.responseStart - entry.requestStart,
          downloadMs: entry.responseEnd - entry.responseStart,
          postResponseMs: performance.now() - entry.responseEnd,
        }
      : {}),
  };
  void upload([], [sample]).then((sent) => {
    if (sent) void migrateLegacySamples();
  });
}

export function captureChatImageError(img: HTMLImageElement): void {
  void upload(
    [],
    [
      {
        path: new URL(img.src, window.location.href).pathname,
        at: new Date().toISOString(),
        error: true,
      },
    ]
  );
}

async function upload(loads: object[], images: object[]): Promise<boolean> {
  if (typeof fetch !== 'function') return false;
  try {
    const response = await fetch('/api/chat/telemetry', {
      method: 'POST',
      credentials: 'same-origin',
      keepalive: true,
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify({ loads, images }),
    });
    if (response.ok) return true;
    console.warn('chat telemetry upload failed', response.status);
  } catch {
    console.warn('chat telemetry upload failed');
  }
  return false;
}

function readLegacy(key: string): object[] {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? '[]');
    return Array.isArray(value) ? value.slice(-20) : [];
  } catch {
    return [];
  }
}

async function migrateLegacySamples(): Promise<void> {
  if (migratingLegacy) return;
  const pendingLoads = readLegacy(LEGACY_PENDING_LOADS_KEY);
  const pendingImages = readLegacy(LEGACY_PENDING_IMAGES_KEY);
  const loads = pendingLoads.length ? pendingLoads : readLegacy(LEGACY_LOADS_KEY);
  const images = pendingImages.length ? pendingImages : readLegacy(LEGACY_IMAGES_KEY);
  if (loads.length === 0 && images.length === 0) return;
  migratingLegacy = true;
  try {
    if (await upload(loads, images)) {
      for (const key of [
        LEGACY_LOADS_KEY,
        LEGACY_IMAGES_KEY,
        LEGACY_PENDING_LOADS_KEY,
        LEGACY_PENDING_IMAGES_KEY,
      ]) {
        sessionStorage.removeItem(key);
      }
    }
  } catch {
    // Storage can be unavailable; new measurements still go directly to the daemon.
  } finally {
    migratingLegacy = false;
  }
}

if (typeof window !== 'undefined') {
  void migrateLegacySamples();
}
