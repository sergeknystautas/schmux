/**
 * TOOLTIP COMPONENT - USAGE EXAMPLES
 *
 * This file demonstrates how to use the Tooltip component throughout the app.
 * Import and wrap any interactive element to add intelligent, accessible tooltips.
 */

import Tooltip from './Tooltip.jsx';

/* =============================================================================
 * BASIC USAGE
 * ============================================================================= */

// Simple text tooltip
<Tooltip content="View session details">
  <button className="btn">
    <svg>...</svg>
  </button>
</Tooltip>

// Tooltip with custom placement (default: 'top')
<Tooltip content="Copy to clipboard" placement="right">
  <button onClick={handleCopy}>
    <svg>...</svg>
  </button>
</Tooltip>

/* =============================================================================
 * VARIANTS
 * ============================================================================= */

// Warning tooltip - for cautionary actions
<Tooltip content="This will permanently delete the session" variant="warning">
  <button className="btn btn--danger">
    Delete
  </button>
</Tooltip>

// Error tooltip - for error states
<Tooltip content="Failed to connect to daemon" variant="error">
  <span className="status-pill status-pill--error">
    Error
  </span>
</Tooltip>

// Default tooltip - standard info
<Tooltip content="Last activity 2 minutes ago" variant="default">
  <span>2m ago</span>
</Tooltip>

/* =============================================================================
 * COMMON PATTERNS
 * ============================================================================= */

// Icon button with tooltip
<Tooltip content="Toggle sidebar">
  <button className="icon-btn" aria-label="Toggle sidebar">
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 12h18M3 6h18M3 18h18"/>
    </svg>
  </button>
</Tooltip>

// Small status indicator with detailed explanation
<Tooltip content="New output since you last viewed this session">
  <span className="badge badge--indicator">New</span>
</Tooltip>

// Truncated text with full content
<Tooltip content={sessionData.repo}>
  <span className="metadata-field__value metadata-field__value--mono">
    {truncateStart(sessionData.repo)}
  </span>
</Tooltip>

// Relative time with absolute timestamp
<Tooltip content={formatTimestamp(sessionData.created_at)}>
  <span>{formatRelativeTime(sessionData.created_at)}</span>
</Tooltip>

// Git status indicator with detailed info
<Tooltip content={`${behind} commits behind origin, ${ahead} commits ahead`}>
  <span className="workspace-item__git-status">
    {behind} | {ahead}
  </span>
</Tooltip>

// Copy button with feedback
<Tooltip content={copied ? "Copied!" : "Copy attach command"} delay={150}>
  <button className="btn btn--sm btn--ghost" onClick={handleCopy}>
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
    </svg>
  </button>
</Tooltip>

/* =============================================================================
 * MIGRATION GUIDE
 * ============================================================================= */

// OLD - native title attribute (inconsistent browser styling)
<button title="View session">View</button>

// NEW - Tooltip component (consistent, accessible, styled)
<Tooltip content="View session">
  <button>View</button>
</Tooltip>

// OLD - broken CSS-only tooltip with data-tooltip attribute
<span className="tooltip" data-tooltip="Uncommitted changes">●</span>

// NEW - working Tooltip component
<Tooltip content="Uncommitted changes">
  <span>●</span>
</Tooltip>

/* =============================================================================
 * ACCESSIBILITY NOTES
 * ============================================================================= */

/**
 * The Tooltip component automatically:
 * - Adds aria-describedby when visible
 * - Shows on focus (keyboard navigation)
 * - Shows on hover
 * - Hides on Escape key
 * - Hides on blur/mouseleave
 *
 * For icon buttons, always provide aria-label:
 *
 * <Tooltip content="View session">
 *   <button aria-label="View session">
 *     <svg>...</svg>
 *   </button>
 * </Tooltip>
 */

/* =============================================================================
 * PLACEMENT REFERENCE
 * ============================================================================= */

/**
 * placement="top"    - Shows above the trigger (default)
 * placement="bottom" - Shows below the trigger
 * placement="left"   - Shows to the left of the trigger
 * placement="right"  - Shows to the right of the trigger
 *
 * The component will auto-adjust if the preferred placement
 * would overflow the viewport.
 */

/* =============================================================================
 * VARIANT REFERENCE
 * ============================================================================= */

/**
 * variant="default" - Dark background, light text (standard)
 * variant="warning" - Light background, dark text, warning border
 * variant="error"   - Light background, error color text, error border
 */

export {
  Tooltip,
};
