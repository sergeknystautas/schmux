import { useState, useEffect } from 'react';

export interface VersionInfo {
  version: string;
  latest_version?: string;
  update_available?: boolean;
  check_error?: string;
}

export default function useVersionInfo() {
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let mounted = true;

    const fetchVersionInfo = async () => {
      try {
        const response = await fetch('/api/healthz');
        if (response.ok) {
          const data = await response.json();
          if (mounted) {
            setVersionInfo(data);
          }
        }
      } catch (err) {
        // Silently fail - version info is nice-to-have
      } finally {
        if (mounted) {
          setLoading(false);
        }
      }
    };

    fetchVersionInfo();

    return () => {
      mounted = false;
    };
  }, []);

  return { versionInfo, loading };
}
