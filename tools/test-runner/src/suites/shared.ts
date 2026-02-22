import { exec, projectRoot } from '../exec.js';
import type { EventCallback } from '../types.js';

let localBuildDone = false;
let dashboardBuildDone = false;

export async function buildLocalArtifacts(onEvent: EventCallback): Promise<boolean> {
  if (localBuildDone) return true;

  const root = projectRoot();
  await exec({ cmd: 'mkdir', args: ['-p', 'build'], cwd: root });

  onEvent('e2e', { type: 'build_step', message: 'Cross-compiling schmux for Linux...' });
  const schmux = await exec({
    cmd: 'go',
    args: ['build', '-o', 'build/schmux-linux', './cmd/schmux'],
    cwd: root,
    env: { GOOS: 'linux', GOARCH: 'amd64', CGO_ENABLED: '0' },
  });
  if (schmux.exitCode !== 0) {
    onEvent('e2e', { type: 'build_step', message: 'Failed to cross-compile schmux' });
    return false;
  }
  onEvent('e2e', { type: 'build_step', message: 'Binary built: build/schmux-linux' });

  onEvent('e2e', { type: 'build_step', message: 'Cross-compiling E2E test binary for Linux...' });
  const e2eBin = await exec({
    cmd: 'go',
    args: ['test', '-tags=e2e', '-c', '-o', 'build/e2e-test', './internal/e2e'],
    cwd: root,
    env: { GOOS: 'linux', GOARCH: 'amd64', CGO_ENABLED: '0' },
  });
  if (e2eBin.exitCode !== 0) {
    onEvent('e2e', { type: 'build_step', message: 'Failed to cross-compile E2E test binary' });
    return false;
  }
  onEvent('e2e', { type: 'build_step', message: 'E2E test binary built: build/e2e-test' });

  localBuildDone = true;
  return true;
}

export async function buildDashboard(onEvent: EventCallback): Promise<boolean> {
  if (dashboardBuildDone) return true;

  const root = projectRoot();

  // Check if node_modules exists to optionally skip install
  const dashboardArgs = ['run', './cmd/build-dashboard'];
  const { existsSync } = await import('node:fs');
  const { resolve } = await import('node:path');
  if (existsSync(resolve(root, 'assets/dashboard/node_modules'))) {
    dashboardArgs.push('--skip-install');
  }

  onEvent('scenarios', { type: 'build_step', message: 'Building dashboard...' });
  const result = await exec({
    cmd: 'go',
    args: dashboardArgs,
    cwd: root,
    env: { VITE_EXPOSE_TERMINAL: 'true' },
  });

  if (result.exitCode !== 0) {
    onEvent('scenarios', { type: 'build_step', message: 'Failed to build dashboard' });
    return false;
  }

  onEvent('scenarios', { type: 'build_step', message: 'Dashboard built' });
  dashboardBuildDone = true;
  return true;
}
