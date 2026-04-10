import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { startLoreCuration } from '../lib/api';
import { useSessions } from './SessionsContext';
import { useToast } from '../components/ToastProvider';
import type { CuratorStreamEvent } from '../lib/types';

interface CurationState {
  phase: string;
  message: string;
  elapsed: number;
  startTime: number;
  status?: string;
  error?: string;
  proposalId?: string;
  events?: CuratorStreamEvent[];
}

type CompletionResult = {
  event_type: string;
  proposalId?: string;
  error?: string;
};

type CurationContextValue = {
  activeCurations: Record<string, CurationState>;
  /** Repos where startCuration has been called but no WebSocket events have arrived yet. */
  pendingCurations: Set<string>;
  startCuration: (repoName: string) => void;
  /** Register a callback for when curation completes (done or error). Returns unsubscribe fn. */
  onComplete: (cb: (repoName: string, result: CompletionResult) => void) => () => void;
  /** Monotonic counter incremented when proposal state changes (dismiss, apply, curation complete). */
  proposalVersion: number;
  /** Bump proposalVersion to trigger re-fetches of proposal counts. */
  invalidateProposals: () => void;
};

const CurationContext = createContext<CurationContextValue | null>(null);

export function CurationProvider({ children }: { children: React.ReactNode }) {
  const { curatorEvents } = useSessions();
  const { success: toastSuccess, error: toastError } = useToast();
  const completionCallbacksRef = useRef<Set<(repoName: string, result: CompletionResult) => void>>(
    new Set()
  );
  // Track which repos we've already fired completion for
  const completedReposRef = useRef<Set<string>>(new Set());
  // Track repos where startCuration was called but no WebSocket events arrived yet
  const [pendingCurations, setPendingCurations] = useState<Set<string>>(new Set());
  // Monotonic counter: bump to signal that proposal counts may have changed
  const [proposalVersion, setProposalVersion] = useState(0);
  const invalidateProposals = useCallback(() => setProposalVersion((v) => v + 1), []);

  // Derive active curations from WebSocket events
  const activeCurations = useMemo(() => {
    const result: Record<string, CurationState> = {};
    for (const [repo, events] of Object.entries(curatorEvents)) {
      if (events.length === 0) continue;
      const last = events[events.length - 1];
      const first = events[0];
      const startTime = new Date(first.timestamp).getTime();

      if (last.event_type === 'curator_done' || last.event_type === 'curator_error') {
        // Terminal state — not active, but keep briefly for completion detection
        continue;
      }

      result[repo] = {
        phase: last.event_type,
        message: last.subtype || last.event_type,
        elapsed: Math.floor((Date.now() - startTime) / 1000),
        startTime,
        events,
      };
    }
    return result;
  }, [curatorEvents]);

  // Detect completion events and notify callbacks
  useEffect(() => {
    for (const [repo, events] of Object.entries(curatorEvents)) {
      if (events.length === 0) continue;
      const last = events[events.length - 1];

      if (last.event_type === 'curator_done' && !completedReposRef.current.has(repo)) {
        completedReposRef.current.add(repo);
        setPendingCurations((prev) => {
          if (!prev.has(repo)) return prev;
          const next = new Set(prev);
          next.delete(repo);
          return next;
        });
        const raw = last.raw as Record<string, unknown>;
        const proposalId = raw.proposal_id as string | undefined;
        toastSuccess(
          proposalId
            ? `Lore: proposal ${proposalId} created`
            : `Lore: curation complete for ${repo}`
        );
        completionCallbacksRef.current.forEach((cb) =>
          cb(repo, {
            event_type: 'curator_done',
            proposalId,
          })
        );
        // New proposal was created — bump version so sidebar re-fetches counts
        invalidateProposals();
      } else if (last.event_type === 'curator_error' && !completedReposRef.current.has(repo)) {
        completedReposRef.current.add(repo);
        setPendingCurations((prev) => {
          if (!prev.has(repo)) return prev;
          const next = new Set(prev);
          next.delete(repo);
          return next;
        });
        const raw = last.raw as Record<string, unknown>;
        const errorMsg = raw.error as string | undefined;
        toastError(`Lore curation failed for ${repo}: ${errorMsg || 'unknown error'}`);
        completionCallbacksRef.current.forEach((cb) =>
          cb(repo, {
            event_type: 'curator_error',
            error: errorMsg,
          })
        );
      }
    }
  }, [curatorEvents, toastSuccess, toastError, invalidateProposals]);

  const startCuration = useCallback(
    async (repoName: string) => {
      if (activeCurations[repoName]) return;
      // Clear completion tracking so we can detect the new run's completion
      completedReposRef.current.delete(repoName);
      // Mark as pending immediately so the UI shows feedback before WebSocket events arrive
      setPendingCurations((prev) => new Set(prev).add(repoName));
      try {
        const response = await startLoreCuration(repoName);
        if (response.status === 'no_raw_entries') {
          setPendingCurations((prev) => {
            const next = new Set(prev);
            next.delete(repoName);
            return next;
          });
          completionCallbacksRef.current.forEach((cb) =>
            cb(repoName, {
              event_type: 'curator_error',
              error: 'No raw entries to curate',
            })
          );
        }
        // Otherwise events will arrive via WebSocket
      } catch (err) {
        setPendingCurations((prev) => {
          const next = new Set(prev);
          next.delete(repoName);
          return next;
        });
        completionCallbacksRef.current.forEach((cb) =>
          cb(repoName, {
            event_type: 'curator_error',
            error: err instanceof Error ? err.message : 'Failed to start curation',
          })
        );
      }
    },
    [activeCurations]
  );

  const onComplete = useCallback((cb: (repoName: string, result: CompletionResult) => void) => {
    completionCallbacksRef.current.add(cb);
    return () => {
      completionCallbacksRef.current.delete(cb);
    };
  }, []);

  return (
    <CurationContext.Provider
      value={{
        activeCurations,
        pendingCurations,
        startCuration,
        onComplete,
        proposalVersion,
        invalidateProposals,
      }}
    >
      {children}
    </CurationContext.Provider>
  );
}

export function useCuration() {
  const ctx = useContext(CurationContext);
  if (!ctx) throw new Error('useCuration must be used within CurationProvider');
  return ctx;
}
