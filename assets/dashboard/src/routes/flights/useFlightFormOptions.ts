import { useMemo } from 'react';
import { useConfig } from '../../contexts/ConfigContext';
import { targetSupportsChat } from '../../lib/chatCapability';
import type { FlightFormTarget } from './FlightForm';

/**
 * The repositories and chat-capable targets a flight form can choose from,
 * derived from config the same way the spawn page does. Shared by the list
 * page (create and edit) and the detail page (edit).
 */
export default function useFlightFormOptions() {
  const { config, loading } = useConfig();
  const repos = config?.repos ?? [];
  const repoNames = useMemo(
    () => Object.fromEntries((config?.repos ?? []).map((r) => [r.url, r.name])),
    [config?.repos]
  );
  const targets = useMemo<FlightFormTarget[]>(() => {
    if (!config?.chat_sessions) return [];
    const enabled = config.enabled_models || {};
    const hasExplicit = Object.keys(enabled).length > 0;
    return (config.models ?? [])
      .filter((m) => (hasExplicit ? m.id in enabled : m.configured))
      .filter((m) => targetSupportsChat(m.id, config.models, config.runners))
      .map((m) => ({ id: m.id, label: m.display_name }));
  }, [config]);
  return { repos, repoNames, targets, loading };
}
