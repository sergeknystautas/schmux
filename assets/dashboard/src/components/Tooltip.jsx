import React, { useState, useRef, useEffect } from 'react';

/**
 * Tooltip Component
 *
 * A refined, accessible tooltip with intelligent positioning and keyboard support.
 *
 * @param {Object} props
 * @param {React.ReactNode} props.children - The trigger element (must accept ref and onMouseEnter/Leave)
 * @param {string} props.content - The tooltip content
 * @param {'top' | 'bottom' | 'left' | 'right'} props.placement - Preferred placement
 * @param {'default' | 'warning' | 'error'} props.variant - Visual variant
 * @param {number} props.delay - Delay before showing (ms)
 * @param {string} props.className - Additional classes for the trigger wrapper
 */
export default function Tooltip({
  children,
  content,
  placement = 'top',
  variant = 'default',
  delay = 300,
  className = '',
}) {
  const [isVisible, setIsVisible] = useState(false);
  const [position, setPosition] = useState({ top: 0, left: 0, arrowOffset: 0 });
  const [actualPlacement, setActualPlacement] = useState(placement);
  const triggerRef = useRef(null);
  const tooltipRef = useRef(null);
  const timeoutRef = useRef(null);

  const showTooltip = () => {
    timeoutRef.current = setTimeout(() => {
      setIsVisible(true);
    }, delay);
  };

  const hideTooltip = () => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
    }
    setIsVisible(false);
  };

  const handleKeyDown = (e) => {
    if (e.key === 'Escape' && isVisible) {
      hideTooltip();
    }
    if ((e.key === 'Enter' || e.key === ' ') && !isVisible) {
      e.preventDefault();
      showTooltip();
    }
  };

  // Calculate position
  useEffect(() => {
    if (!isVisible || !triggerRef.current || !tooltipRef.current) return;

    const trigger = triggerRef.current.getBoundingClientRect();
    const tooltip = tooltipRef.current.getBoundingClientRect();
    const scrollX = window.scrollX || window.pageXOffset;
    const scrollY = window.scrollY || window.pageYOffset;
    const gap = 8; // Space between trigger and tooltip

    // Get viewport dimensions
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    // Calculate positions for each placement
    const positions = {
      top: {
        top: trigger.top + scrollY - tooltip.height - gap,
        left: trigger.left + scrollX + (trigger.width - tooltip.width) / 2,
      },
      bottom: {
        top: trigger.bottom + scrollY + gap,
        left: trigger.left + scrollX + (trigger.width - tooltip.width) / 2,
      },
      left: {
        top: trigger.top + scrollY + (trigger.height - tooltip.height) / 2,
        left: trigger.left + scrollX - tooltip.width - gap,
      },
      right: {
        top: trigger.top + scrollY + (trigger.height - tooltip.height) / 2,
        left: trigger.right + scrollX + gap,
      },
    };

    // Calculate arrow position to center it on the trigger
    // For top/bottom: where is the trigger's center relative to tooltip's left edge?
    // For left/right: where is the trigger's center relative to tooltip's top edge?
    const getArrowPosition = (placement, pos) => {
      const triggerCenter = trigger.left + trigger.width / 2;
      const triggerCenterY = trigger.top + trigger.height / 2;

      switch (placement) {
        case 'top':
        case 'bottom':
          // Distance from tooltip's left edge to trigger's center
          return triggerCenter - pos.left;
        case 'left':
        case 'right':
          // Distance from tooltip's top edge to trigger's center
          return triggerCenterY - pos.top;
        default:
          return 0;
      }
    };

    // Check if preferred placement fits in viewport
    const fitsInViewport = (pos) => {
      return (
        pos.top >= 0 &&
        pos.left >= 0 &&
        pos.top + tooltip.height <= viewportHeight &&
        pos.left + tooltip.width <= viewportWidth
      );
    };

    // Try preferred placement first
    let finalPlacement = placement;
    let finalPos = positions[placement];

    // If it doesn't fit, try alternative placements in order
    const placementOrder = {
      top: ['top', 'bottom', 'left', 'right'],
      bottom: ['bottom', 'top', 'left', 'right'],
      left: ['left', 'right', 'top', 'bottom'],
      right: ['right', 'left', 'top', 'bottom'],
    };

    for (const tryPlacement of placementOrder[placement]) {
      const tryPos = positions[tryPlacement];
      if (fitsInViewport(tryPos)) {
        finalPlacement = tryPlacement;
        finalPos = tryPos;
        break;
      }
    }

    setActualPlacement(finalPlacement);
    setPosition({
      top: finalPos.top,
      left: finalPos.left,
      arrowOffset: getArrowPosition(finalPlacement, finalPos),
    });
  }, [isVisible, placement]);

  // Cleanup timeout
  useEffect(() => {
    return () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  // Clone child and add event handlers
  const trigger = React.cloneElement(children, {
    ref: triggerRef,
    onMouseEnter: showTooltip,
    onMouseLeave: hideTooltip,
    onFocus: showTooltip,
    onBlur: hideTooltip,
    onKeyDown: handleKeyDown,
    'aria-describedby': isVisible ? 'tooltip-content' : undefined,
  });

  const variantClasses = {
    default: 'tooltip-react--default',
    warning: 'tooltip-react--warning',
    error: 'tooltip-react--error',
  };

  const placementArrowClasses = {
    top: 'tooltip-react__arrow--top',
    bottom: 'tooltip-react__arrow--bottom',
    left: 'tooltip-react__arrow--left',
    right: 'tooltip-react__arrow--right',
  };

  return (
    <>
      {trigger}
      {isVisible && (
        <div
          ref={tooltipRef}
          role="tooltip"
          id="tooltip-content"
          className={`tooltip-react ${variantClasses[variant]}`}
          style={{
            position: 'absolute',
            top: position.top,
            left: position.left,
            zIndex: 1000,
          }}
        >
          <span className="tooltip-react__content">{content}</span>
          <span
            className={`tooltip-react__arrow ${placementArrowClasses[actualPlacement]}`}
            style={
              actualPlacement === 'top' || actualPlacement === 'bottom'
                ? { left: `${position.arrowOffset - 5}px` }
                : { top: `${position.arrowOffset - 5}px` }
            }
          />
        </div>
      )}
    </>
  );
}
