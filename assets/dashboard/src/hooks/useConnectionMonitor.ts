import { useEffect, useState } from 'react';

const CHECK_INTERVAL = 5000;

export default function useConnectionMonitor() {
  const [connected, setConnected] = useState(true);

  useEffect(() => {
    let active = true;
    let intervalId: number | undefined;

    const check = async () => {
      try {
        const response = await fetch('/api/healthz');
        if (!active) return;
        setConnected(response.ok);
      } catch {
        if (!active) return;
        setConnected(false);
      }
    };

    check();
    intervalId = window.setInterval(check, CHECK_INTERVAL);

    return () => {
      active = false;
      if (intervalId) window.clearInterval(intervalId);
    };
  }, []);

  return connected;
}
