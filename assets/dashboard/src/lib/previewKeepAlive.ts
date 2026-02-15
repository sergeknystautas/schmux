// Store both the iframe and the canonical URL we used to create it
// This avoids browser URL normalization issues when comparing
const iframeRegistry = new Map<
  string,
  { iframe: HTMLIFrameElement; url: string; lastUsed: number }
>();
const MAX_IFRAMES = 10;
let parkingLot: HTMLDivElement | null = null;

function ensureParkingLot(): HTMLDivElement {
  if (parkingLot) return parkingLot;
  parkingLot = document.createElement('div');
  parkingLot.id = 'preview-iframe-parking';
  parkingLot.style.position = 'fixed';
  parkingLot.style.left = '-200vw';
  parkingLot.style.top = '-200vh';
  parkingLot.style.width = '1px';
  parkingLot.style.height = '1px';
  parkingLot.style.zIndex = '10';
  parkingLot.style.pointerEvents = 'none';
  parkingLot.style.overflow = 'hidden';
  parkingLot.setAttribute('aria-hidden', 'true');
  document.body.appendChild(parkingLot);
  return parkingLot;
}

function evictLeastRecentlyUsed(): void {
  if (iframeRegistry.size < MAX_IFRAMES) return;
  let oldestId: string | null = null;
  let oldestTime = Infinity;
  for (const [id, entry] of iframeRegistry) {
    if (entry.lastUsed < oldestTime) {
      oldestTime = entry.lastUsed;
      oldestId = id;
    }
  }
  if (oldestId) {
    const evicted = iframeRegistry.get(oldestId);
    if (evicted) {
      evicted.iframe.remove();
      iframeRegistry.delete(oldestId);
    }
  }
}

function ensurePreviewIframe(previewId: string, url: string): HTMLIFrameElement {
  const lot = ensureParkingLot();
  const entry = iframeRegistry.get(previewId);

  if (!entry) {
    // Evict least recently used iframe if at capacity
    evictLeastRecentlyUsed();

    // Create new iframe
    const iframe = document.createElement('iframe');
    iframe.src = url;
    iframe.setAttribute(
      'sandbox',
      'allow-scripts allow-same-origin allow-forms allow-popups allow-downloads'
    );
    iframe.style.position = 'absolute';
    iframe.style.inset = '0';
    iframe.style.width = '100%';
    iframe.style.height = '100%';
    iframe.style.border = 'none';
    iframe.style.visibility = 'hidden';
    iframeRegistry.set(previewId, { iframe, url, lastUsed: Date.now() });
    lot.appendChild(iframe);
    return iframe;
  }

  // Update last used timestamp
  entry.lastUsed = Date.now();

  // Only reload if the URL has actually changed
  // Compare against our stored URL, not iframe.src (which is browser-normalized)
  if (entry.url !== url) {
    entry.iframe.src = url;
    entry.url = url;
  }

  return entry.iframe;
}

export function showPreviewIframe(
  previewId: string,
  url: string,
  viewport: { left: number; top: number; width: number; height: number }
): void {
  const lot = ensureParkingLot();
  ensurePreviewIframe(previewId, url);

  lot.style.left = `${Math.max(0, viewport.left)}px`;
  lot.style.top = `${Math.max(0, viewport.top)}px`;
  lot.style.width = `${Math.max(1, viewport.width)}px`;
  lot.style.height = `${Math.max(1, viewport.height)}px`;
  lot.style.pointerEvents = 'auto';

  for (const [id, entry] of iframeRegistry) {
    const active = id === previewId;
    entry.iframe.style.visibility = active ? 'visible' : 'hidden';
    entry.iframe.style.pointerEvents = active ? 'auto' : 'none';
  }
}

export function hidePreviewIframes(): void {
  const lot = ensureParkingLot();
  lot.style.left = '-200vw';
  lot.style.top = '-200vh';
  lot.style.width = '1px';
  lot.style.height = '1px';
  lot.style.pointerEvents = 'none';
}

export function removePreviewIframe(previewId: string): void {
  const entry = iframeRegistry.get(previewId);
  if (!entry) return;
  entry.iframe.remove();
  iframeRegistry.delete(previewId);
}
