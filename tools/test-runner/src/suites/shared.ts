import { arch } from 'node:os';
import { copyFileSync } from 'node:fs';
import { exec, projectRoot } from '../exec.js';
import type { EventCallback } from '../types.js';

let localBuildDone = false;
let localBuildCoverage = false;
let dashboardBuildDone = false;
let dashboardBuildCoverage = false;

/** Force the next buildLocalArtifacts call to recompile the binary. */
export function invalidateLocalBuild(): void {
  localBuildDone = false;
}

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

export async function buildLocalArtifacts(
  onEvent: EventCallback,
  coverage = false
): Promise<BuildResult> {
  if (localBuildDone && localBuildCoverage === coverage) return { ok: true, errorOutput: '' };

  const root = projectRoot();
  await exec({ cmd: 'mkdir', args: ['-p', 'build'], cwd: root });

  const ga = goArch();
  const coverArgs = coverage ? ['-cover', '-coverpkg=./...'] : [];
  const coverLabel = coverage ? ' (with coverage)' : '';
  onEvent('e2e', {
    type: 'build_step',
    message: `Cross-compiling schmux for linux/${ga}${coverLabel}...`,
  });
  const schmux = await exec({
    cmd: 'go',
    args: ['build', ...coverArgs, '-o', 'build/schmux-linux', './cmd/schmux'],
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
  localBuildCoverage = coverage;
  return { ok: true, errorOutput: '' };
}

export async function buildDashboard(
  onEvent: EventCallback,
  coverage = false
): Promise<BuildResult> {
  if (dashboardBuildDone && dashboardBuildCoverage === coverage) {
    return { ok: true, errorOutput: '' };
  }

  const root = projectRoot();

  // Skip npm install only if node_modules is in sync with package-lock.json.
  // A stale node_modules (e.g. missing newly-added deps) causes typecheck failures.
  const dashboardArgs = ['run', './cmd/build-dashboard'];
  const { existsSync, readFileSync } = await import('node:fs');
  const { resolve } = await import('node:path');
  const lockFile = resolve(root, 'assets/dashboard/package-lock.json');
  const marker = resolve(root, 'assets/dashboard/node_modules/.package-lock-marker');
  if (existsSync(marker)) {
    try {
      const currentLock = readFileSync(lockFile, 'utf8');
      const cachedLock = readFileSync(marker, 'utf8');
      if (currentLock === cachedLock) {
        dashboardArgs.push('--skip-install');
      }
    } catch {
      // marker or lock unreadable — run install
    }
  }

  onEvent('scenarios', { type: 'build_step', message: 'Building dashboard...' });
  const buildEnv: Record<string, string> = { VITE_EXPOSE_TERMINAL: 'true' };
  if (coverage) {
    buildEnv['VITE_COVERAGE'] = 'true';
  }
  const result = await exec({
    cmd: 'go',
    args: dashboardArgs,
    cwd: root,
    env: buildEnv,
  });

  if (result.exitCode !== 0) {
    onEvent('scenarios', { type: 'build_step', message: 'Failed to build dashboard' });
    return { ok: false, errorOutput: extractBuildError(result) };
  }

  onEvent('scenarios', { type: 'build_step', message: 'Dashboard built' });
  // Write marker so next run can skip install if lock file hasn't changed
  if (!dashboardArgs.includes('--skip-install')) {
    try {
      copyFileSync(lockFile, marker);
    } catch {
      // non-fatal
    }
  }
  dashboardBuildDone = true;
  dashboardBuildCoverage = coverage;
  return { ok: true, errorOutput: '' };
}
