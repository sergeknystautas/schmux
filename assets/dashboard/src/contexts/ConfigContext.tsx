import React, { createContext, useState, useContext, useEffect, useMemo, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getConfig, getErrorMessage } from '../lib/api';
import type { ConfigResponse } from '../lib/types';
import { CONFIG_UPDATED_KEY } from '../lib/constants';

type ConfigContextValue = {
  config: ConfigResponse;
  loading: boolean;
  error: string | null;
  isNotConfigured: boolean;
  isFirstRun: boolean;
  completeFirstRun: () => void;
  reloadConfig: () => Promise<void>;
  getRepoName: (repoUrl: string) => string;
};

const ConfigContext = createContext<ConfigContextValue | null>(null);

const DEFAULT_CONFIG: ConfigResponse = {
  workspace_path: '',
  source_code_management: 'git-worktree',
  repos: [],
  run_targets: [],
  models: [],
  quick_launch: [],
  nudgenik: { target: '', viewed_buffer_ms: 5000, seen_interval_ms: 2000 },
  branch_suggest: { target: '' },
  conflict_resolve: { target: '', timeout_ms: 120000 },
  terminal: {
    width: 120,
    height: 40,
    seed_lines: 100,
    bootstrap_lines: 20000,
  },
  sessions: {
    dashboard_poll_interval_ms: 5000,
    git_status_poll_interval_ms: 10000,
    git_clone_timeout_ms: 300000,
    git_status_timeout_ms: 30000,
  },
  xterm: {
    mtime_poll_interval_ms: 5000,
    query_timeout_ms: 5000,
    operation_timeout_ms: 10000,
    max_log_size_mb: 50,
    rotated_log_size_mb: 1,
  },
  network: {
    bind_address: '127.0.0.1',
    port: 7337,
    public_base_url: '',
    tls: {
      cert_path: '',
      key_path: '',
    },
  },
  access_control: {
    enabled: false,
    provider: 'github',
    session_ttl_minutes: 1440,
  },
  pr_review: {
    target: '',
  },
  commit_message: {
    target: '',
  },
  notifications: {
    sound_disabled: false,
    confirm_before_close: false,
  },
  needs_restart: false,
};

export function ConfigProvider({ children }: { children: React.ReactNode }) {
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
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
      setError(getErrorMessage(err, 'Failed to load config'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  // Listen for config changes from other tabs via localStorage
  useEffect(() => {
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === CONFIG_UPDATED_KEY) {
        loadConfig();
      }
    };
    window.addEventListener('storage', handleStorageChange);
    return () => window.removeEventListener('storage', handleStorageChange);
  }, [loadConfig]);

  // Compute whether app is configured
  // App is "not configured" if: empty workspace path or no repos
  const isNotConfigured = useMemo(() => {
    if (loading || error) return false;
    const wsPath = config?.workspace_path || '';
    return !wsPath.trim() || !config?.repos || config.repos.length === 0;
  }, [config, loading, error]);

  // Helper to get repo name from URL
  const getRepoName = useCallback(
    (repoUrl: string) => {
      if (!repoUrl) return repoUrl;
      const repo = config?.repos?.find((r) => r.url === repoUrl);
      return repo?.name || repoUrl;
    },
    [config?.repos]
  );

  const value = useMemo(
    () => ({
      config,
      loading,
      error,
      isNotConfigured,
      isFirstRun,
      completeFirstRun: () => setIsFirstRun(false),
      reloadConfig: loadConfig,
      getRepoName,
    }),
    [config, loading, error, isNotConfigured, isFirstRun, loadConfig, getRepoName]
  );

  return <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>;
}

export function useConfig() {
  const ctx = useContext(ConfigContext);
  if (!ctx) {
    throw new Error('useConfig must be used within a ConfigProvider');
  }
  return ctx;
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
