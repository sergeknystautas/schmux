import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// The previewKeepAlive module uses module-scoped state (iframeRegistry Map, parkingLot ref).
// We must re-import the module in each test to get a clean registry.
// Using dynamic imports with vi.resetModules() ensures fresh state per test.

function cleanupParkingLot(): void {
  const lot = document.getElementById('preview-iframe-parking');
  if (lot) lot.remove();
}

describe('previewKeepAlive', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();
    cleanupParkingLot();
  });

  afterEach(() => {
    vi.useRealTimers();
    cleanupParkingLot();
  });

  async function loadModule() {
    return await import('./previewKeepAlive');
  }

  describe('showPreviewIframe', () => {
    it('creates a parking lot div on first call', async () => {
      const { showPreviewIframe } = await loadModule();
      expect(document.getElementById('preview-iframe-parking')).toBeNull();

      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking');
      expect(lot).not.toBeNull();
      expect(lot!.getAttribute('aria-hidden')).toBe('true');
    });

    it('creates an iframe with the correct src', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const iframes = lot.querySelectorAll('iframe');
      expect(iframes).toHaveLength(1);
      expect(iframes[0].src).toBe('http://localhost:3000/');
    });

    it('sets sandbox attribute on iframes', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const iframe = lot.querySelector('iframe')!;
      expect(iframe.getAttribute('sandbox')).toBe(
        'allow-scripts allow-forms allow-popups allow-downloads allow-same-origin'
      );
    });

    it('positions the parking lot according to viewport', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 100,
        top: 50,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.style.left).toBe('100px');
      expect(lot.style.top).toBe('50px');
      expect(lot.style.width).toBe('800px');
      expect(lot.style.height).toBe('600px');
    });

    it('clamps negative viewport values to 0', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: -10,
        top: -20,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.style.left).toBe('0px');
      expect(lot.style.top).toBe('0px');
    });

    it('clamps small dimensions to 1px minimum', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 0,
        height: -5,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.style.width).toBe('1px');
      expect(lot.style.height).toBe('1px');
    });

    it('makes only the active iframe visible', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });
      showPreviewIframe('p2', 'http://localhost:3001', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const iframes = lot.querySelectorAll('iframe');
      expect(iframes).toHaveLength(2);

      const p1Iframe = Array.from(iframes).find((f) => f.src.includes('3000'))!;
      const p2Iframe = Array.from(iframes).find((f) => f.src.includes('3001'))!;

      expect(p2Iframe.style.visibility).toBe('visible');
      expect(p2Iframe.style.pointerEvents).toBe('auto');
      expect(p1Iframe.style.visibility).toBe('hidden');
      expect(p1Iframe.style.pointerEvents).toBe('none');
    });

    it('reuses existing iframe when same previewId is shown again', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 10,
        top: 20,
        width: 900,
        height: 700,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.querySelectorAll('iframe')).toHaveLength(1);
    });

    it('updates iframe src when URL changes for same previewId', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });
      showPreviewIframe('p1', 'http://localhost:4000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const iframe = lot.querySelector('iframe')!;
      expect(iframe.src).toBe('http://localhost:4000/');
    });

    it('enables pointer events on parking lot when showing', async () => {
      const { showPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.style.pointerEvents).toBe('auto');
    });
  });

  describe('hidePreviewIframes', () => {
    it('moves parking lot off-screen', async () => {
      const { showPreviewIframe, hidePreviewIframes } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 100,
        top: 50,
        width: 800,
        height: 600,
      });

      hidePreviewIframes();

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.style.left).toBe('-200vw');
      expect(lot.style.top).toBe('-200vh');
      expect(lot.style.width).toBe('1px');
      expect(lot.style.height).toBe('1px');
      expect(lot.style.pointerEvents).toBe('none');
    });

    it('does not remove iframes from the registry', async () => {
      const { showPreviewIframe, hidePreviewIframes } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      hidePreviewIframes();

      // Show again - should reuse the existing iframe (only 1 iframe total)
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.querySelectorAll('iframe')).toHaveLength(1);
    });
  });

  describe('removePreviewIframe', () => {
    it('removes the iframe from the DOM', async () => {
      const { showPreviewIframe, removePreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.querySelectorAll('iframe')).toHaveLength(1);

      removePreviewIframe('p1');
      expect(lot.querySelectorAll('iframe')).toHaveLength(0);
    });

    it('is a no-op for unknown previewId', async () => {
      const { showPreviewIframe, removePreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      removePreviewIframe('unknown');

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.querySelectorAll('iframe')).toHaveLength(1);
    });

    it('allows creating a new iframe after removal', async () => {
      const { showPreviewIframe, removePreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });
      removePreviewIframe('p1');
      showPreviewIframe('p1', 'http://localhost:4000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const iframes = lot.querySelectorAll('iframe');
      expect(iframes).toHaveLength(1);
      expect(iframes[0].src).toBe('http://localhost:4000/');
    });
  });

  describe('refreshPreviewIframe', () => {
    it('resets the iframe src to trigger reload', async () => {
      const { showPreviewIframe, refreshPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const iframe = lot.querySelector('iframe')!;

      // Manually change src to simulate navigation
      iframe.src = 'http://localhost:3000/other-page';

      refreshPreviewIframe('p1');

      // Should be reset to the stored URL
      expect(iframe.src).toBe('http://localhost:3000/');
    });

    it('is a no-op for unknown previewId', async () => {
      const { refreshPreviewIframe } = await loadModule();
      // Should not throw
      refreshPreviewIframe('unknown');
    });
  });

  describe('goBackPreviewIframe', () => {
    it('is a no-op for unknown previewId', async () => {
      const { goBackPreviewIframe } = await loadModule();
      // Should not throw
      goBackPreviewIframe('unknown');
    });

    it('does not throw for cross-origin restrictions', async () => {
      const { showPreviewIframe, goBackPreviewIframe } = await loadModule();
      showPreviewIframe('p1', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      // The function has a try/catch for cross-origin, so this should not throw
      goBackPreviewIframe('p1');
    });
  });

  describe('LRU eviction', () => {
    it('evicts the least recently used iframe when at capacity', async () => {
      const { showPreviewIframe } = await loadModule();

      // Create MAX_IFRAMES (10) iframes
      for (let i = 0; i < 10; i++) {
        vi.setSystemTime(new Date(2024, 0, 1, 0, 0, i));
        showPreviewIframe(`p${i}`, `http://localhost:${3000 + i}`, {
          left: 0,
          top: 0,
          width: 800,
          height: 600,
        });
      }

      const lot = document.getElementById('preview-iframe-parking')!;
      expect(lot.querySelectorAll('iframe')).toHaveLength(10);

      // Adding one more should evict the oldest (p0)
      vi.setSystemTime(new Date(2024, 0, 1, 0, 0, 10));
      showPreviewIframe('p10', 'http://localhost:3010', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      expect(lot.querySelectorAll('iframe')).toHaveLength(10);

      // p0 should be gone (port 3000)
      const srcs = Array.from(lot.querySelectorAll('iframe')).map((f) => f.src);
      expect(srcs).not.toContain('http://localhost:3000/');
      expect(srcs).toContain('http://localhost:3010/');
    });

    it('updates lastUsed timestamp when accessing existing iframe', async () => {
      const { showPreviewIframe } = await loadModule();

      // Create 10 iframes
      for (let i = 0; i < 10; i++) {
        vi.setSystemTime(new Date(2024, 0, 1, 0, 0, i));
        showPreviewIframe(`p${i}`, `http://localhost:${3000 + i}`, {
          left: 0,
          top: 0,
          width: 800,
          height: 600,
        });
      }

      // Touch p0 (the oldest) to make it recently used
      vi.setSystemTime(new Date(2024, 0, 1, 0, 1, 0));
      showPreviewIframe('p0', 'http://localhost:3000', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      // Now add a new iframe - p1 should be evicted (it's now the oldest)
      vi.setSystemTime(new Date(2024, 0, 1, 0, 2, 0));
      showPreviewIframe('p10', 'http://localhost:3010', {
        left: 0,
        top: 0,
        width: 800,
        height: 600,
      });

      const lot = document.getElementById('preview-iframe-parking')!;
      const srcs = Array.from(lot.querySelectorAll('iframe')).map((f) => f.src);

      // p0 should still be present (was recently touched)
      expect(srcs).toContain('http://localhost:3000/');
      // p1 should be evicted (oldest untouched)
      expect(srcs).not.toContain('http://localhost:3001/');
    });
  });
});
