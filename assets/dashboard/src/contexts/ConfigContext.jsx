import React, { createContext, useState, useContext, useEffect, useMemo, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getConfig } from '../lib/api.js';

const ConfigContext = createContext();

const DEFAULT_CONFIG = {
  internal: {
    mtime_poll_interval_ms: 5000,
    sessions_poll_interval_ms: 5000,
    viewed_buffer_ms: 5000,
    session_seen_interval_ms: 2000,
  }
};

export function ConfigProvider({ children }) {
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [isFirstRun, setIsFirstRun] = useState(false);

  const loadConfig = useCallback(async () => {
    try {
      const data = await getConfig();
      setConfig(data);
      // Set isFirstRun if workspace_path is empty on initial load
      if (!data?.workspace_path?.trim()) {
        setIsFirstRun(true);
      }
      setError(null);
    } catch (err) {
      console.error('Failed to load config:', err);
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let active = true;

    loadConfig();

    return () => {
      active = false;
    };
  }, [loadConfig]);

  // Compute whether app is configured
  // App is "not configured" if: empty workspace path or no repos
  const isNotConfigured = useMemo(() => {
    if (loading || error) return false;
    const wsPath = config?.workspace_path || '';
    return !wsPath.trim() ||
           !config?.repos ||
           config.repos.length === 0;
  }, [config, loading, error]);

  // Helper to get repo name from URL
  const getRepoName = useCallback((repoUrl) => {
    if (!repoUrl) return repoUrl;
    const repo = config?.repos?.find(r => r.url === repoUrl);
    return repo?.name || repoUrl;
  }, [config?.repos]);

  const value = useMemo(() => ({
    config,
    loading,
    error,
    isNotConfigured,
    isFirstRun,
    completeFirstRun: () => setIsFirstRun(false),
    reloadConfig: loadConfig,
    getRepoName,
  }), [config, loading, error, isNotConfigured, isFirstRun, loadConfig, getRepoName]);

  return (
    <ConfigContext.Provider value={value}>
      {children}
    </ConfigContext.Provider>
  );
}

export function useConfig() {
  return useContext(ConfigContext);
}

// Hook to redirect to /config if not configured
export function useRequireConfig() {
  const { isNotConfigured, loading } = useConfig();
  const navigate = useNavigate();

  useEffect(() => {
    if (!loading && isNotConfigured) {
      navigate('/config', { replace: true });
    }
  }, [isNotConfigured, loading, navigate]);
}
