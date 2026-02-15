import React from 'react';

export function formatTimestamp(timestamp: string | number | Date): string {
  const date = new Date(timestamp);
  return date.toLocaleString();
}

export function formatRelativeTime(timestamp: string | number | Date): string {
  const date = new Date(timestamp);
  const now = new Date();
  const diff = now.getTime() - date.getTime();

  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (seconds < 60) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  if (days < 30) return `${days}d ago`;
  return date.toLocaleDateString();
}

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function truncateStart(text: string, maxLength = 40): string {
  if (text.length <= maxLength) return text;
  const suffix = text.slice(-maxLength + 3);
  return '...' + suffix;
}

/**
 * Shared nudge state emoji map used in AppShell sidebar and SessionTabs.
 */
export const nudgeStateEmoji: Record<string, string> = {
  'Needs Authorization': '\u26D4\uFE0F',
  'Needs Feature Clarification': '\uD83D\uDD0D',
  'Needs User Testing': '\uD83D\uDC40',
  Completed: '\u2705',
  Error: '\u274C',
};

/**
 * Format a nudge summary string, truncating if necessary.
 * @param summary The nudge summary text
 * @param maxLength Maximum length before truncation (default 100)
 */
export function formatNudgeSummary(summary?: string, maxLength = 100): string | null {
  if (!summary) return null;
  let text = summary.trim();
  if (text.length > maxLength) {
    text = text.substring(0, maxLength - 3) + '...';
  }
  return text;
}

/**
 * A small CSS-animated spinner for "Working..." indicators.
 */
export function WorkingSpinner(): React.ReactElement {
  return React.createElement('span', { className: 'working-spinner' });
}
