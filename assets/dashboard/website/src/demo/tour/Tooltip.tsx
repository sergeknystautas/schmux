import { useEffect, useState, useCallback } from 'react';
import type { TourStep } from './types';

interface TooltipProps {
  step: TourStep;
  currentStep: number;
  totalSteps: number;
  onNext: () => void;
  onPrev: () => void;
  onEnd: () => void;
}

export default function Tooltip({
  step,
  currentStep,
  totalSteps,
  onNext,
  onPrev,
  onEnd,
}: TooltipProps) {
  const [rect, setRect] = useState<DOMRect | null>(null);

  const updateRect = useCallback(() => {
    const el = document.querySelector(step.target);
    setRect(el ? el.getBoundingClientRect() : null);
  }, [step.target]);

  // Watch for target element to appear and reposition on DOM changes
  useEffect(() => {
    updateRect();
    window.addEventListener('scroll', updateRect, true);
    window.addEventListener('resize', updateRect);
    const observer = new MutationObserver(updateRect);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true });
    return () => {
      window.removeEventListener('scroll', updateRect, true);
      window.removeEventListener('resize', updateRect);
      observer.disconnect();
    };
  }, [updateRect]);

  if (!rect) return null;

  const style = computePosition(rect, step.placement);

  return (
    <div className="tour-tooltip" style={style} role="dialog" aria-label={step.title}>
      <div className="tour-tooltip__header">
        <span className="tour-tooltip__title">{step.title}</span>
        <button className="tour-tooltip__close" onClick={onEnd} aria-label="Close tour">
          &times;
        </button>
      </div>
      <p className="tour-tooltip__body">{step.body}</p>
      <div className="tour-tooltip__footer">
        <span className="tour-tooltip__progress">
          {currentStep + 1} / {totalSteps}
        </span>
        <div className="tour-tooltip__actions">
          {currentStep > 0 && (
            <button className="tour-tooltip__btn tour-tooltip__btn--back" onClick={onPrev}>
              Back
            </button>
          )}
          {step.advanceOn === 'next' && (
            <button className="tour-tooltip__btn tour-tooltip__btn--next" onClick={onNext}>
              {currentStep === totalSteps - 1 ? 'Done' : 'Next'}
            </button>
          )}
          {step.advanceOn === 'click' && (
            <span className="tour-tooltip__hint">Click the highlighted element</span>
          )}
        </div>
      </div>
    </div>
  );
}

export function computePosition(
  rect: DOMRect,
  placement: TourStep['placement']
): React.CSSProperties {
  const gap = 12;
  const tooltipWidth = 320;
  const edgePadding = 16;

  // Clamp a left value so the tooltip stays within the viewport
  const clampLeft = (left: number) =>
    Math.max(edgePadding, Math.min(left, window.innerWidth - tooltipWidth - edgePadding));

  switch (placement) {
    case 'bottom':
      return {
        position: 'fixed',
        top: rect.bottom + gap,
        left: clampLeft(rect.left),
        maxWidth: tooltipWidth,
      };
    case 'top':
      return {
        position: 'fixed',
        bottom: window.innerHeight - rect.top + gap,
        left: clampLeft(rect.left),
        maxWidth: tooltipWidth,
      };
    case 'right':
      return { position: 'fixed', top: rect.top, left: rect.right + gap, maxWidth: tooltipWidth };
    case 'left':
      return {
        position: 'fixed',
        top: rect.top,
        right: window.innerWidth - rect.left + gap,
        maxWidth: tooltipWidth,
      };
  }
}
