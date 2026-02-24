import { arch } from 'node:os';
import { exec, projectRoot } from '../exec.js';
import type { EventCallback } from '../types.js';

let localBuildDone = false;
let dashboardBuildDone = false;

/** Map Node.js os.arch() to Go's GOARCH values */
function goArch(): string {
  const a = arch();
  if (a === 'arm64') return 'arm64';
  return 'amd64'; // x64 and others default to amd64
}

export interface BuildResult {
  ok: boolean;
  /** On failure, the tail of the build command's output */
  errorOutput: string;
}

/** Extract the last N lines of a failed command's combined output */
function extractBuildError(result: { stdout: string; stderr: string }, maxLines = 30): string {
  // Combine both streams — stdout often has the real errors (e.g. tsc output),
  // stderr has the wrapper's exit message. Show both for full context.
  const parts = [result.stdout.trim(), result.stderr.trim()].filter(Boolean);
  const combined = parts.join('\n');
  if (!combined) return '';
  const lines = combined.split('\n');
  return lines.slice(-maxLines).join('\n');
}

export async function buildLocalArtifacts(onEvent: EventCallback): Promise<BuildResult> {
  if (localBuildDone) return { ok: true, errorOutput: '' };

  const root = projectRoot();
  await exec({ cmd: 'mkdir', args: ['-p', 'build'], cwd: root });

  const ga = goArch();
  onEvent('e2e', { type: 'build_step', message: `Cross-compiling schmux for linux/${ga}...` });
  const schmux = await exec({
    cmd: 'go',
    args: ['build', '-o', 'build/schmux-linux', './cmd/schmux'],
    cwd: root,
    env: { GOOS: 'linux', GOARCH: ga, CGO_ENABLED: '0' },
  });
  if (schmux.exitCode !== 0) {
    onEvent('e2e', { type: 'build_step', message: 'Failed to cross-compile schmux' });
    return { ok: false, errorOutput: extractBuildError(schmux) };
  }
  onEvent('e2e', { type: 'build_step', message: 'Binary built: build/schmux-linux' });

  onEvent('e2e', {
    type: 'build_step',
    message: `Cross-compiling E2E test binary for linux/${ga}...`,
  });
  const e2eBin = await exec({
    cmd: 'go',
    args: ['test', '-tags=e2e', '-c', '-o', 'build/e2e-test', './internal/e2e'],
    cwd: root,
    env: { GOOS: 'linux', GOARCH: ga, CGO_ENABLED: '0' },
  });
  if (e2eBin.exitCode !== 0) {
    onEvent('e2e', { type: 'build_step', message: 'Failed to cross-compile E2E test binary' });
    return { ok: false, errorOutput: extractBuildError(e2eBin) };
  }
  onEvent('e2e', { type: 'build_step', message: 'E2E test binary built: build/e2e-test' });

  localBuildDone = true;
  return { ok: true, errorOutput: '' };
}

export async function buildDashboard(onEvent: EventCallback): Promise<BuildResult> {
  if (dashboardBuildDone) return { ok: true, errorOutput: '' };

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
    return { ok: false, errorOutput: extractBuildError(result) };
  }

  onEvent('scenarios', { type: 'build_step', message: 'Dashboard built' });
  dashboardBuildDone = true;
  return { ok: true, errorOutput: '' };
}
