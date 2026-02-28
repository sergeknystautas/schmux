import { useEffect, useState, useCallback } from 'react';

interface SpotlightProps {
  target: string; // CSS selector
  active: boolean;
  padding?: number;
}

export default function Spotlight({ target, active, padding = 8 }: SpotlightProps) {
  const [rect, setRect] = useState<DOMRect | null>(null);

  const updateRect = useCallback(() => {
    const el = document.querySelector(target);
    setRect(el ? el.getBoundingClientRect() : null);
  }, [target]);

  useEffect(() => {
    if (!active) return;
    updateRect();
    // Re-measure on scroll, resize, and DOM mutations
    window.addEventListener('scroll', updateRect, true);
    window.addEventListener('resize', updateRect);
    const observer = new MutationObserver(updateRect);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true });
    return () => {
      window.removeEventListener('scroll', updateRect, true);
      window.removeEventListener('resize', updateRect);
      observer.disconnect();
    };
  }, [active, updateRect]);

  if (!active || !rect) return null;

  const x = rect.x - padding;
  const y = rect.y - padding;
  const w = rect.width + padding * 2;
  const h = rect.height + padding * 2;
  const r = 6; // border radius

  // SVG clip-path: full viewport minus rounded rect cutout
  const vw = window.innerWidth;
  const vh = window.innerHeight;

  return (
    <div className="tour-spotlight" aria-hidden="true">
      <svg width={vw} height={vh}>
        <defs>
          <mask id="tour-mask">
            <rect width={vw} height={vh} fill="white" />
            <rect x={x} y={y} width={w} height={h} rx={r} ry={r} fill="black" />
          </mask>
        </defs>
        <rect width={vw} height={vh} fill="rgba(0,0,0,0.6)" mask="url(#tour-mask)" />
      </svg>
    </div>
  );
}
