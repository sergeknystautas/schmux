import type { Transport } from '@dashboard/lib/transport';
import type { WorkspaceResponse } from '@dashboard/lib/types';
import type { TerminalRecording } from '../recordings/types';
import { MockDashboardWebSocket, MockTerminalWebSocket } from './MockWebSocket';
import { createDemoConfig, createDemoDiff, createDemoGitGraph } from './mockData';

export interface DemoTransportOptions {
  /** Initial workspace state */
  workspaces: WorkspaceResponse[];
  /** Terminal recordings keyed by session ID */
  recordings: Record<string, TerminalRecording>;
}

export function createDemoTransport(options: DemoTransportOptions): Transport & {
  /** Update the workspace state (triggers re-broadcast to dashboard WS) */
  updateWorkspaces: (workspaces: WorkspaceResponse[]) => void;
  /** Get the terminal playback for a session */
  getTerminalWS: (sessionId: string) => MockTerminalWebSocket | undefined;
} {
  let currentWorkspaces = options.workspaces;
  let dashboardWS: MockDashboardWebSocket | null = null;
  const terminalSockets = new Map<string, MockTerminalWebSocket>();

  const transport: Transport & {
    updateWorkspaces: (ws: WorkspaceResponse[]) => void;
    getTerminalWS: (sid: string) => MockTerminalWebSocket | undefined;
  } = {
    createWebSocket(url: string): WebSocket {
      if (url.includes('/ws/dashboard')) {
        dashboardWS = new MockDashboardWebSocket(url);
        // Send initial state after connection opens
        setTimeout(() => {
          dashboardWS?.pushMessage({ type: 'sessions', workspaces: currentWorkspaces });
        }, 100);
        return dashboardWS as unknown as WebSocket;
      }

      if (url.includes('/ws/terminal/')) {
        const sessionId = url.split('/ws/terminal/')[1];
        const mockWS = new MockTerminalWebSocket(url);
        terminalSockets.set(sessionId, mockWS);
        // Start playback after connection opens
        const recording = options.recordings[sessionId];
        if (recording) {
          setTimeout(() => mockWS.startPlayback(recording), 150);
        }
        return mockWS as unknown as WebSocket;
      }

      // Fallback: return a no-op WebSocket
      return new MockDashboardWebSocket(url) as unknown as WebSocket;
    },

    fetch(input: RequestInfo | URL, _init?: RequestInit): Promise<Response> {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

      // Config endpoint
      if (url.includes('/api/config')) {
        return Promise.resolve(
          new Response(JSON.stringify(createDemoConfig()), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Sessions endpoint
      if (url.includes('/api/sessions')) {
        return Promise.resolve(
          new Response(JSON.stringify(currentWorkspaces), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Spawn endpoint (return success)
      if (url.includes('/api/spawn')) {
        return Promise.resolve(
          new Response(JSON.stringify({ sessions: [{ id: 'demo-sess-new' }] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Detect tools
      if (url.includes('/api/detect-tools')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              tools: [
                { name: 'claude', command: 'claude', source: 'detected' },
                { name: 'codex', command: 'codex', source: 'detected' },
              ],
            }),
            {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            }
          )
        );
      }

      // Recent branches — returns RecentBranch[]
      if (url.includes('/api/recent-branches')) {
        return Promise.resolve(
          new Response(JSON.stringify([]), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Overlays — returns OverlaysResponse
      if (url.includes('/api/overlays')) {
        return Promise.resolve(
          new Response(JSON.stringify({ overlays: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // PRs — returns PRsResponse
      if (url.includes('/api/prs')) {
        return Promise.resolve(
          new Response(JSON.stringify({ prs: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Subreddit — returns SubredditResponse
      if (url.includes('/api/subreddit')) {
        return Promise.resolve(
          new Response(JSON.stringify({ enabled: false }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Autolearn batches — returns batches list
      if (url.includes('/api/autolearn/')) {
        return Promise.resolve(
          new Response(JSON.stringify({ batches: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Personas — returns PersonaListResponse
      if (url.includes('/api/personas')) {
        return Promise.resolve(
          new Response(JSON.stringify({ personas: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Remote flavor statuses — returns RemoteFlavorStatus[]
      if (url.includes('/api/remote/flavor-statuses')) {
        return Promise.resolve(
          new Response(JSON.stringify([]), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Suggest branch — returns SuggestBranchResponse
      if (url.includes('/api/suggest-branch')) {
        return Promise.resolve(
          new Response(JSON.stringify({ branch: 'feature/demo-branch', nickname: 'demo-branch' }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Diff endpoint — returns DiffResponse with mock file diffs
      if (url.includes('/api/diff/')) {
        const workspaceId = url.split('/api/diff/')[1]?.split('?')[0];
        return Promise.resolve(
          new Response(JSON.stringify(createDemoDiff(workspaceId || 'demo-ws-1')), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Commit graph endpoint — returns CommitGraphResponse with commit history
      if (url.includes('/commit-graph')) {
        const match = url.match(/\/api\/workspaces\/([^/]+)\/commit-graph/);
        const workspaceId = match?.[1] || 'demo-ws-1';
        return Promise.resolve(
          new Response(JSON.stringify(createDemoGitGraph(decodeURIComponent(workspaceId))), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        );
      }

      // Default: return 200 empty
      return Promise.resolve(
        new Response('{}', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      );
    },

    updateWorkspaces(workspaces: WorkspaceResponse[]) {
      currentWorkspaces = workspaces;
      dashboardWS?.pushMessage({ type: 'sessions', workspaces });
    },

    getTerminalWS(sessionId: string) {
      return terminalSockets.get(sessionId);
    },
  };

  return transport;
}
