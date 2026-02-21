// Browser Notification API wrapper for escalation alerts.

let permissionRequested = false;

/** Request notification permission. Idempotent — skips if already requested, granted, or denied. */
export function requestNotificationPermission(): void {
  if (permissionRequested) return;
  if (!('Notification' in window)) return;
  if (Notification.permission === 'granted' || Notification.permission === 'denied') return;

  permissionRequested = true;
  Notification.requestPermission();
}

/** Show a browser notification if the tab is not focused. */
export function showBrowserNotification(title: string, body: string): void {
  if (!('Notification' in window)) return;
  if (Notification.permission !== 'granted') return;
  if (document.hasFocus()) return;

  const notification = new Notification(title, {
    body,
    icon: '/schmux-icon.png',
    tag: 'schmux-escalation', // Replaces previous escalation notification
  });

  // Auto-close after 10 seconds
  setTimeout(() => notification.close(), 10_000);

  // Focus the tab when clicked
  notification.onclick = () => {
    window.focus();
    notification.close();
  };
}
