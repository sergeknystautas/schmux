import React, { useState, useCallback, useEffect, useRef } from 'react';
import { Box, Text, useApp, useStdout } from 'ink';
import { StatusBar } from './components/StatusBar.js';
import { LogPanel } from './components/LogPanel.js';
import { KeyBar } from './components/KeyBar.js';
import { useProcess } from './hooks/useProcess.js';
import { useKeyboard } from './hooks/useKeyboard.js';
import { build } from './lib/build.js';
import { killPort } from './lib/ports.js';
import {
  ensureSchmuxDir,
  readRestartManifest,
  writeDevState,
  readDaemonPid,
  cleanupStateFiles,
  paths,
} from './lib/state.js';
import { checkDependencies, npmInstall } from './lib/deps.js';
import type { ProcessStatus } from './types.js';

interface AppProps {
  devRoot: string;
}

const MAX_LOG_LINES = 500;
const VITE_PORT = 5173;
const FLUSH_INTERVAL_MS = 19; // ~90fps — batches log lines to reduce Ink re-renders

export function App({ devRoot }: AppProps) {
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
  const [backendStatusOverride, setBackendStatusOverride] = useState<ProcessStatus | null>(null);
  const binaryPath = `${devRoot}/tmp/schmux`;
  const workspaceRef = useRef(workspace);
  workspaceRef.current = workspace;

  // Buffer incoming log lines in refs, flush to state on a timer to reduce re-renders
  const backendBuf = useRef<string[]>([]);
  const frontendBuf = useRef<string[]>([]);

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
    }, FLUSH_INTERVAL_MS);
    return () => clearInterval(id);
  }, []);

  const addBackendLine = useCallback((line: string) => {
    backendBuf.current.push(line);
  }, []);

  const addFrontendLine = useCallback((line: string) => {
    frontendBuf.current.push(line);
  }, []);

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

  const handleClear = useCallback(() => {
    backendBuf.current = [];
    frontendBuf.current = [];
    setBackendLines([]);
    setFrontendLines([]);
  }, []);

  const handleQuit = useCallback(async () => {
    await backend.stop();
    await frontend.stop();
    await cleanupStateFiles();
    exit();
  }, [backend, frontend, exit]);

  const canRestart = phase === 'running' && effectiveBackendStatus !== 'building';

  useKeyboard({ onRestart: handleRestart, onClear: handleClear, onQuit: handleQuit, canRestart });

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

  return (
    <Box flexDirection="column" height={termHeight}>
      <StatusBar
        devRoot={devRoot}
        workspace={workspace}
        backendStatus={effectiveBackendStatus}
        frontendStatus={frontend.status}
      />
      <Box flexDirection="row" flexGrow={1}>
        <LogPanel title="Frontend" lines={frontendLines} />
        <LogPanel title="Backend" lines={backendLines} />
      </Box>
      <KeyBar canRestart={canRestart} />
    </Box>
  );
}
