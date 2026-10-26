import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest';
import { act, render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import DemoShell, { createScenarioSetup } from '../../website/src/demo/DemoShell';
import ChatView from './chat/ChatView';
import ModalProvider from './ModalProvider';
import ToastProvider from './ToastProvider';
import { clientPerf } from '../lib/clientPerf';
import { setTransport, liveTransport } from '../lib/transport';

// The session page mounts a terminal; xterm cannot render in jsdom.
vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    loadAddon = vi.fn();
    open = vi.fn();
    onData = vi.fn();
    onRender = vi.fn();
    onScroll = vi.fn();
    onWriteParsed = vi.fn();
    onTitleChange = vi.fn();
    writeln = vi.fn();
    write = vi.fn();
    clear = vi.fn();
    reset = vi.fn();
    resize = vi.fn();
    focus = vi.fn();
    scrollToBottom = vi.fn();
    dispose = vi.fn();
    element = null;
    buffer = {
      active: { viewportY: 0, baseY: 0, cursorY: 0, cursorX: 0, length: 0, getLine: () => null },
    };
    rows = 24;
    cols = 80;
    markers = [];
    unicode = { activeVersion: '6' };
    parser = {
      registerCsiHandler: vi.fn(() => ({ dispose: vi.fn() })),
      registerEscHandler: vi.fn(() => ({ dispose: vi.fn() })),
    };
  }
  return { Terminal: MockTerminal };
});
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: vi.fn() }));
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: vi.fn() }));
vi.mock('@xterm/addon-webgl', () => ({
  WebglAddon: vi.fn().mockImplementation(() => ({ onContextLoss: vi.fn(), dispose: vi.fn() })),
}));
// The demo transport serves neither the auth probe nor timelapse recordings.
vi.mock('../lib/api', async (importOriginal) => {
  const original = await importOriginal<typeof import('../lib/api')>();
  return {
    ...original,
    getAuthMe: () => Promise.resolve({ status: 'disabled' }),
    getTimelapseRecordings: () => Promise.resolve([]),
  };
});

describe('production Profiler ids', () => {
  let recordCommit: MockInstance<typeof clientPerf.recordCommit>;
  const ids = () => new Set(recordCommit.mock.calls.map(([id]) => id));

  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(console, 'debug').mockImplementation(() => {});
    // jsdom has no scrollIntoView; the sidebar scrolls the active session into view.
    Element.prototype.scrollIntoView = vi.fn();
    recordCommit = vi.spyOn(clientPerf, 'recordCommit');
  });

  afterEach(() => {
    vi.restoreAllMocks();
    setTransport(liveTransport);
  });

  it('records commits from all four production Profiler ids', async () => {
    await act(async () => {
      render(
        <MemoryRouter initialEntries={['/sessions/demo-sess-1']}>
          <DemoShell setup={createScenarioSetup('workspaces')} />
        </MemoryRouter>
      );
    });
    // The session page mounts once the demo sessions arrive over the mock socket.
    await waitFor(() => expect(ids()).toContain('SessionDetailPage'));

    render(
      <ToastProvider>
        <ModalProvider>
          <ChatView
            conversation={{
              items: [],
              phase: 'idle',
              activity: {
                operations: {},
                toolIndex: {},
                order: [],
                checklist: {},
                checklistOrder: [],
                pendingInput: [],
                attentionOutcomes: [],
                live: true,
              },
            }}
            status="connected"
            ended={false}
            historyLoaded
            socketError={null}
            onSend={() => true}
            onInterrupt={() => {}}
            onPermission={() => {}}
            onAnswer={() => {}}
            onAbort={() => {}}
            signedOut={false}
            signedOutProtocol=""
            onReauth={() => {}}
          />
        </ModalProvider>
      </ToastProvider>
    );

    expect([...ids()].sort()).toEqual(['SessionDetailPage', 'chat-transcript', 'main', 'sidebar']);
  });
});
