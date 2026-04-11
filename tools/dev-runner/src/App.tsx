import React, { useState, useCallback, useEffect, useRef } from 'react';
import { Box, Static, Text, useApp, useStdout } from 'ink';
import { StatusBar } from './components/StatusBar.js';
import { LogPanel } from './components/LogPanel.js';
import { KeyBar } from './components/KeyBar.js';
import { useProcess } from './hooks/useProcess.js';
import { useKeyboard } from './hooks/useKeyboard.js';
import { build } from './lib/build.js';
import { cleanEnv } from './lib/cleanEnv.js';
import { gitPull } from './lib/git.js';
import { killPort } from './lib/ports.js';
import {
  ensureSchmuxDir,
  readRestartManifest,
  writeDevState,
  readDaemonPid,
  readConfigPort,
  cleanupStateFiles,
  paths,
} from './lib/state.js';
import { checkDependencies, npmInstall } from './lib/deps.js';
import type { ProcessStatus } from './types.js';

interface AppProps {
  devRoot: string;
  plain: boolean;
}

interface PlainLine {
  id: number;
  source: 'fe' | 'be';
  text: string;
}

const MAX_LOG_LINES = 500;
const VITE_PORT = 5173;
const FLUSH_INTERVAL_MS = 19; // ~90fps — batches log lines to reduce Ink re-renders

export function App({ devRoot, plain }: AppProps) {
  const { exit } = useApp();
  const { stdout } = useStdout();
  const termHeight = stdout?.rows ?? 24;
  const [workspace, setWorkspace] = useState(devRoot);
  const [frontendLines, setFrontendLines] = useState<string[]>([]);
  const [backendLines, setBackendLines] = useState<string[]>([]);
  const [phase, setPhase] = useState<'init' | 'building' | 'starting' | 'running' | 'error'>(
    'init'
  );
  const [errorMsg, setErrorMsg] = useState('');
  const [dashboardPort, setDashboardPort] = useState(7337);
  const [backendStatusOverride, setBackendStatusOverride] = useState<ProcessStatus | null>(null);
  const [layout, setLayout] = useState<'horizontal' | 'vertical'>('vertical');
  const [logLevel, setLogLevel] = useState('info');
  const binaryPath = `${devRoot}/tmp/schmux`;
  const workspaceRef = useRef(workspace);
  workspaceRef.current = workspace;

  // Buffer incoming log lines in refs, flush to state on a timer to reduce re-renders
  const backendBuf = useRef<string[]>([]);
  const frontendBuf = useRef<string[]>([]);

  // Plain mode: combined interleaved line buffer
  const plainBuf = useRef<PlainLine[]>([]);
  const plainIdRef = useRef(0);
  const [plainLines, setPlainLines] = useState<PlainLine[]>([]);

  useEffect(() => {
    const id = setInterval(() => {
      if (backendBuf.current.length > 0) {
        const lines = backendBuf.current;
        backendBuf.current = [];
        setBackendLines((prev) => {
          const next = [...prev, ...lines];
          return next.length > MAX_LOG_LINES ? next.slice(-MAX_LOG_LINES) : next;
        });
      }
      if (frontendBuf.current.length > 0) {
        const lines = frontendBuf.current;
        frontendBuf.current = [];
        setFrontendLines((prev) => {
          const next = [...prev, ...lines];
          return next.length > MAX_LOG_LINES ? next.slice(-MAX_LOG_LINES) : next;
        });
      }
      if (plain && plainBuf.current.length > 0) {
        const lines = plainBuf.current;
        plainBuf.current = [];
        setPlainLines((prev) => [...prev, ...lines]);
      }
    }, FLUSH_INTERVAL_MS);
    return () => clearInterval(id);
  }, [plain]);

  const addBackendLine = useCallback(
    (line: string) => {
      backendBuf.current.push(line);
      if (plain) {
        plainBuf.current.push({ id: plainIdRef.current++, source: 'be', text: line });
      }
    },
    [plain]
  );

  const addFrontendLine = useCallback(
    (line: string) => {
      frontendBuf.current.push(line);
      if (plain) {
        plainBuf.current.push({ id: plainIdRef.current++, source: 'fe', text: line });
      }
    },
    [plain]
  );

  // Handle daemon exit — check for exit code 42 (workspace switch)
  const handleDaemonExit = useCallback(
    async (code: number) => {
      if (code !== 42) return;

      addBackendLine('Dev restart requested (exit code 42)');
      const manifest = await readRestartManifest();
      if (!manifest || !manifest.workspace_path) {
        addBackendLine('No valid restart manifest, restarting with current binary');
        // Will be restarted by the effect below
        return;
      }

      const newWorkspace = manifest.workspace_path;
      addBackendLine(`Switching to workspace: ${newWorkspace} (type: ${manifest.type})`);

      if (manifest.type === 'backend' || manifest.type === 'both') {
        setBackendStatusOverride('building');
        addBackendLine('Rebuilding...');
        const result = await build(newWorkspace, binaryPath, addBackendLine);
        if (result.success) {
          setWorkspace(newWorkspace);
          await writeDevState({ source_workspace: newWorkspace });
          addBackendLine('Build succeeded');
        } else {
          addBackendLine('Build failed, restarting with previous binary');
        }
        setBackendStatusOverride(null);
      }

      if (manifest.type === 'frontend' || manifest.type === 'both') {
        setWorkspace(newWorkspace);
        await writeDevState({ source_workspace: newWorkspace });
        // Vite restart is handled by the workspace-change useEffect above
      }

      // Clean up manifest
      const { rm } = await import('node:fs/promises');
      await rm(paths.devRestart, { force: true });

      // Restart daemon
      backend.start();
    },
    [addBackendLine, binaryPath]
  );

  const backend = useProcess({
    command: binaryPath,
    args: ['daemon-run', '--dev-mode', '--dev-proxy'],
    env: cleanEnv(),
    onLine: addBackendLine,
    onExit: handleDaemonExit,
  });

  const frontend = useProcess({
    command: 'npx',
    args: ['vite', '--port', String(VITE_PORT), '--strictPort'],
    cwd: `${workspace}/assets/dashboard`,
    onLine: addFrontendLine,
  });

  // Effective backend status (override during builds)
  const effectiveBackendStatus = backendStatusOverride ?? backend.status;

  // Restart Vite when workspace changes (e.g., after a dev rebuild switch).
  // useProcess doesn't auto-restart on cwd change — it just updates an internal ref.
  // This effect fires after the re-render, so the ref already has the new cwd.
  const prevWorkspaceRef = useRef(workspace);
  useEffect(() => {
    if (prevWorkspaceRef.current === workspace) return;
    prevWorkspaceRef.current = workspace;
    if (frontend.status === 'idle') return; // not started yet
    const dashboardDir = `${workspace}/assets/dashboard`;
    (async () => {
      addFrontendLine(`Syncing npm dependencies in ${dashboardDir}...`);
      try {
        await npmInstall(dashboardDir, addFrontendLine);
      } catch {
        addFrontendLine('npm install failed — Vite may not start correctly');
      }
      addFrontendLine(`Starting Vite from ${dashboardDir}`);
      frontend.restart();
    })();
  }, [workspace, frontend, addFrontendLine]);

  // Startup sequence
  useEffect(() => {
    let cancelled = false;

    async function startup() {
      try {
        // Phase: init — check deps
        const missing = await checkDependencies();
        if (cancelled) return;
        if (missing.length > 0) {
          setErrorMsg(`Missing dependencies: ${missing.join(', ')}`);
          setPhase('error');
          return;
        }

        // Ensure ~/.schmux directory exists
        await ensureSchmuxDir();

        // Read configured port for StatusBar display
        const port = await readConfigPort();
        if (!cancelled) setDashboardPort(port);

        // Stop existing daemon if running
        const existingPid = await readDaemonPid();
        if (existingPid !== null) {
          try {
            process.kill(existingPid, 0); // Check if alive
            addBackendLine(`Stopping existing daemon (PID ${existingPid})...`);
            process.kill(existingPid, 'SIGTERM');
            // Wait for it to die (up to 5 seconds)
            for (let i = 0; i < 50; i++) {
              try {
                process.kill(existingPid, 0);
                await new Promise((r) => setTimeout(r, 100));
              } catch {
                break;
              }
            }
          } catch {
            // Not running
          }
        }

        // Phase: building
        if (cancelled) return;
        setPhase('building');
        addBackendLine('Building Go binary...');

        const { mkdirSync } = await import('node:fs');
        mkdirSync(`${devRoot}/tmp`, { recursive: true });

        const buildResult = await build(devRoot, binaryPath, addBackendLine);
        if (cancelled) return;

        if (!buildResult.success) {
          setErrorMsg('Initial build failed. Fix errors and press r to retry.');
          setPhase('error');
          return;
        }

        addBackendLine('Build succeeded');

        // Phase: starting — launch Vite and daemon concurrently
        setPhase('starting');

        // Kill orphaned Vite processes
        await killPort(VITE_PORT);
        if (cancelled) return;

        // Ensure dashboard npm dependencies are installed
        addFrontendLine('Syncing npm dependencies...');
        try {
          await npmInstall(`${devRoot}/assets/dashboard`, addFrontendLine);
        } catch {
          addFrontendLine('npm install failed — Vite may not start correctly');
        }
        if (cancelled) return;

        // Write initial dev state
        await writeDevState({ source_workspace: devRoot });

        // Start both processes
        frontend.start();
        backend.start();

        setPhase('running');

        // Fetch initial log level from daemon (retry briefly since it's starting up)
        (async () => {
          for (let i = 0; i < 10; i++) {
            try {
              const resp = await fetch(`http://localhost:${port}/api/dev/log-level`);
              if (resp.ok) {
                const data = await resp.json();
                if (!cancelled) setLogLevel(data.level);
                return;
              }
            } catch {
              // daemon not ready yet
            }
            await new Promise((r) => setTimeout(r, 500));
          }
        })();
      } catch (err) {
        if (!cancelled) {
          setErrorMsg(`Startup failed: ${err}`);
          setPhase('error');
        }
      }
    }

    startup();
    return () => {
      cancelled = true;
    };
  }, []); // Runs once on mount

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      cleanupStateFiles().catch(() => {});
    };
  }, []);

  // Keyboard handlers
  const handleRestart = useCallback(async () => {
    setBackendStatusOverride('building');
    addBackendLine('Rebuilding...');
    await backend.stop();
    const result = await build(workspaceRef.current, binaryPath, addBackendLine);
    setBackendStatusOverride(null);
    if (result.success) {
      addBackendLine('Build succeeded, restarting daemon...');
      backend.start();
    } else {
      addBackendLine('Build failed');
    }
  }, [backend, binaryPath, addBackendLine]);

  const handlePull = useCallback(async () => {
    setBackendStatusOverride('pulling');
    addBackendLine('Pulling latest changes...');
    const pullResult = await gitPull(devRoot, addBackendLine);
    if (!pullResult.success) {
      addBackendLine('Pull failed — not restarting');
      setBackendStatusOverride(null);
      return;
    }
    addBackendLine('Pull succeeded, rebuilding...');
    setBackendStatusOverride('building');
    await backend.stop();
    const result = await build(workspaceRef.current, binaryPath, addBackendLine);
    setBackendStatusOverride(null);
    if (result.success) {
      addBackendLine('Build succeeded, restarting daemon...');
      backend.start();
    } else {
      addBackendLine('Build failed');
    }
  }, [backend, binaryPath, addBackendLine, devRoot]);

  const handleClear = useCallback(() => {
    if (plain) return; // can't un-write lines from stdout
    backendBuf.current = [];
    frontendBuf.current = [];
    setBackendLines([]);
    setFrontendLines([]);
  }, [plain]);

  const handleQuit = useCallback(async () => {
    await backend.stop();
    await frontend.stop();
    await cleanupStateFiles();
    exit();
  }, [backend, frontend, exit]);

  const handleToggleLayout = useCallback(() => {
    if (plain) return; // layout not applicable in plain mode
    setLayout((prev) => (prev === 'horizontal' ? 'vertical' : 'horizontal'));
  }, [plain]);

  const handleResetWorkspace = useCallback(async () => {
    if (workspaceRef.current === devRoot) return;
    addBackendLine(`Resetting workspace to dev root: ${devRoot}`);
    setBackendStatusOverride('building');
    await backend.stop();
    const result = await build(devRoot, binaryPath, addBackendLine);
    setBackendStatusOverride(null);
    if (result.success) {
      setWorkspace(devRoot);
      await writeDevState({ source_workspace: devRoot });
      addBackendLine('Build succeeded, restarting daemon...');
      backend.start();
    } else {
      addBackendLine('Build failed');
    }
  }, [backend, binaryPath, addBackendLine, devRoot]);

  const handleToggleLogLevel = useCallback(async () => {
    const next = logLevel === 'debug' ? 'info' : 'debug';
    try {
      const resp = await fetch(`http://localhost:${dashboardPort}/api/dev/log-level`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ level: next }),
      });
      if (resp.ok) {
        const data = await resp.json();
        setLogLevel(data.level);
        addBackendLine(`Log level changed to ${data.level}`);
      } else {
        addBackendLine(`Failed to toggle log level (HTTP ${resp.status})`);
      }
    } catch {
      addBackendLine('Failed to toggle log level (daemon not reachable)');
    }
  }, [logLevel, dashboardPort, addBackendLine]);

  const canRestart =
    phase === 'running' &&
    effectiveBackendStatus !== 'building' &&
    effectiveBackendStatus !== 'pulling';

  const canResetWorkspace = canRestart && workspace !== devRoot;

  useKeyboard({
    onRestart: handleRestart,
    onPull: handlePull,
    onClear: handleClear,
    onQuit: handleQuit,
    onToggleLayout: handleToggleLayout,
    onResetWorkspace: handleResetWorkspace,
    onToggleLogLevel: handleToggleLogLevel,
    canRestart,
    canResetWorkspace,
  });

  // Error screen
  if (phase === 'error') {
    return (
      <Box flexDirection="column" padding={1}>
        <Text bold color="red">
          schmux dev — error
        </Text>
        <Text color="red">{errorMsg}</Text>
        <Text dimColor>
          Press q to quit
          {phase === 'error' && effectiveBackendStatus !== 'building' ? ' or r to retry' : ''}
        </Text>
      </Box>
    );
  }

  // Plain mode: append-only log stream with sticky footer
  if (plain) {
    return (
      <>
        <Static items={plainLines}>
          {(line) => (
            <Text key={line.id}>
              <Text color={line.source === 'fe' ? 'cyan' : 'yellow'}>
                [{line.source === 'fe' ? 'FE' : 'BE'}]
              </Text>{' '}
              {line.text}
            </Text>
          )}
        </Static>
        <Box flexDirection="column">
          <StatusBar
            devRoot={devRoot}
            workspace={workspace}
            backendStatus={effectiveBackendStatus}
            frontendStatus={frontend.status}
            port={dashboardPort}
            logLevel={logLevel}
          />
          <KeyBar canRestart={canRestart} plain canResetWorkspace={canResetWorkspace} />
        </Box>
      </>
    );
  }

  const frontendFlex = 1;
  const backendFlex = 3;
  let frontendMaxLines: number | undefined;
  let backendMaxLines: number | undefined;
  if (layout === 'vertical') {
    // StatusBar(6) + KeyBar(2) + 2×border+title(3) = 14 rows of chrome
    const totalContent = Math.max(6, termHeight - 14);
    const totalFlex = frontendFlex + backendFlex;
    frontendMaxLines = Math.max(3, Math.floor((totalContent * frontendFlex) / totalFlex));
    backendMaxLines = Math.max(3, Math.floor((totalContent * backendFlex) / totalFlex));
  }

  return (
    <Box flexDirection="column" height={termHeight}>
      <StatusBar
        devRoot={devRoot}
        workspace={workspace}
        backendStatus={effectiveBackendStatus}
        frontendStatus={frontend.status}
        port={dashboardPort}
        logLevel={logLevel}
      />
      <Box flexDirection={layout === 'horizontal' ? 'row' : 'column'} flexGrow={1}>
        <LogPanel
          title="Frontend"
          lines={frontendLines}
          layout={layout}
          flex={frontendFlex}
          maxLines={frontendMaxLines}
        />
        <LogPanel
          title="Backend"
          lines={backendLines}
          layout={layout}
          flex={backendFlex}
          maxLines={backendMaxLines}
        />
      </Box>
      <KeyBar canRestart={canRestart} layout={layout} canResetWorkspace={canResetWorkspace} />
    </Box>
  );
}
