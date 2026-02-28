import { useEffect } from 'react';
import { setTransport, liveTransport, transport } from '@dashboard/lib/transport';
import App from '@dashboard/App';
import TourProvider from './tour/TourProvider';
import { createDemoTransport } from './transport/mockTransport';
import { createDemoWorkspaces, createPostSpawnWorkspaces } from './transport/mockData';
import {
  authImplRecording,
  testWriterRecording,
  rateLimiterRecording,
} from './recordings/workspaces';
import { newAgentRecording } from './recordings/spawn';
import { workspacesScenario } from './scenarios/workspaces';
import { spawnScenario } from './scenarios/spawn';
import type { TourScenario } from './tour/types';
import './demo.css';

export interface ScenarioSetup {
  scenario: TourScenario;
  transport: ReturnType<typeof createDemoTransport>;
}

const SCENARIOS: Record<string, () => ScenarioSetup> = {
  workspaces: () => {
    const dt = createDemoTransport({
      workspaces: createDemoWorkspaces(),
      recordings: {
        'demo-sess-1': authImplRecording,
        'demo-sess-2': testWriterRecording,
        'demo-sess-3': rateLimiterRecording,
      },
    });
    return { scenario: workspacesScenario, transport: dt };
  },
  spawn: () => {
    const dt = createDemoTransport({
      workspaces: createDemoWorkspaces(),
      recordings: {
        'demo-sess-1': authImplRecording,
        'demo-sess-2': testWriterRecording,
        'demo-sess-3': rateLimiterRecording,
        'demo-sess-new': newAgentRecording,
      },
    });

    // Wire up spawn scenario hooks that need transport access
    const scenario = { ...spawnScenario };
    scenario.steps = scenario.steps.map((step, i) => {
      if (i === 4) {
        // "Launch the agent" step — after clicking Engage, update state
        return {
          ...step,
          afterStep: () => {
            dt.updateWorkspaces(createPostSpawnWorkspaces());
          },
        };
      }
      if (i === 5) {
        // "Watch it work" step — navigate to new session
        return {
          ...step,
          route: '/sessions/demo-sess-new',
        };
      }
      return step;
    });

    return { scenario, transport: dt };
  },
};

/** Create the scenario setup (transport + tour). Called from main.tsx
 *  BEFORE render so the transport is available when App mounts. */
export function createScenarioSetup(scenarioId: string): ScenarioSetup {
  const factory = SCENARIOS[scenarioId] || SCENARIOS['workspaces'];
  return factory();
}

interface DemoShellProps {
  setup: ScenarioSetup;
}

export default function DemoShell({ setup }: DemoShellProps) {
  // Install transport synchronously during render so it's ready
  // before App's effects fire (useEffect would be too late).
  setTransport(setup.transport);

  // Restore live transport on unmount, but only if this instance's
  // transport is still active (a new DemoShell may have already
  // installed a different transport during scenario switching).
  useEffect(() => {
    const myTransport = setup.transport;
    return () => {
      if (transport === myTransport) {
        setTransport(liveTransport);
      }
    };
  }, [setup]);

  return (
    <TourProvider scenario={setup.scenario}>
      <App />
      <div className="demo-banner">
        <div className="demo-banner__label">
          <span className="demo-banner__badge">Demo</span>
          <span>You're exploring schmux with sample data</span>
        </div>
        <div className="demo-banner__actions">
          <a className="demo-banner__link" href="#/workspaces">
            Workspaces tour
          </a>
          <a className="demo-banner__link" href="#/spawn">
            Spawn tour
          </a>
          <a
            className="demo-banner__link"
            href="https://github.com/sergeknystautas/schmux"
            target="_blank"
            rel="noopener"
          >
            Install schmux →
          </a>
        </div>
      </div>
    </TourProvider>
  );
}
