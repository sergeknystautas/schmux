import { execSync } from 'node:child_process';
import { exec, projectRoot } from './exec.js';
import type { EventCallback, SuiteName } from './types.js';

let _runtime: string | undefined;

export function containerRuntime(): string {
  if (_runtime !== undefined) return _runtime;
  for (const cmd of ['docker', 'podman']) {
    try {
      execSync(`which ${cmd}`, { stdio: 'ignore' });
      _runtime = cmd;
      return cmd;
    } catch {}
  }
  _runtime = 'docker';
  return _runtime;
}

export interface DockerBuildOptions {
  dockerfile: string;
  tag: string;
  verbose?: boolean;
  onEvent?: EventCallback;
  suite?: SuiteName;
}

export interface DockerRunOptions {
  tag: string;
  env?: Record<string, string>;
  volumes?: string[];
  onLine?: (line: string) => void;
  verbose?: boolean;
}

/** Emit the last N lines of a failed command's combined output */
function emitBuildFailure(
  onEvent: EventCallback | undefined,
  suite: SuiteName,
  label: string,
  result: { stdout: string; stderr: string }
): void {
  if (!onEvent) return;
  const combined = (result.stderr || result.stdout).trim();
  if (!combined) return;

  onEvent(suite, { type: 'output_line', line: '' });
  onEvent(suite, { type: 'output_line', line: `── ${label} output ──` });
  const lines = combined.split('\n');
  const tail = lines.slice(-30);
  for (const line of tail) {
    onEvent(suite, { type: 'output_line', line });
  }
  onEvent(suite, { type: 'output_line', line: '' });
}

export async function isDockerAvailable(): Promise<boolean> {
  const result = await exec({ cmd: containerRuntime(), args: ['info'], cwd: projectRoot() });
  return result.exitCode === 0;
}

export async function imageExists(tag: string): Promise<boolean> {
  const result = await exec({
    cmd: containerRuntime(),
    args: ['image', 'inspect', tag],
    cwd: projectRoot(),
  });
  return result.exitCode === 0;
}

export async function ensureBaseImage(opts: {
  tag: string;
  dockerfile: string;
  label: string;
  force: boolean;
  verbose: boolean;
  onEvent?: EventCallback;
  suite?: SuiteName;
}): Promise<boolean> {
  if (!opts.force && (await imageExists(opts.tag))) {
    opts.onEvent?.(opts.suite ?? 'e2e', {
      type: 'build_step',
      message: `Reusing cached ${opts.label} base image (use --force to rebuild)`,
    });
    return true;
  }

  opts.onEvent?.(opts.suite ?? 'e2e', {
    type: 'build_step',
    message: `Building ${opts.label} base image...`,
  });

  const result = await exec({
    cmd: containerRuntime(),
    args: ['build', '-f', opts.dockerfile, '-t', opts.tag, '.'],
    cwd: projectRoot(),
    onLine: opts.verbose
      ? (line) => {
          opts.onEvent?.(opts.suite ?? 'e2e', { type: 'output_line', line });
        }
      : undefined,
  });

  if (result.exitCode !== 0) {
    opts.onEvent?.(opts.suite ?? 'e2e', {
      type: 'build_step',
      message: `Failed to build ${opts.label} base image`,
    });
    if (!opts.verbose) {
      emitBuildFailure(
        opts.onEvent,
        opts.suite ?? 'e2e',
        `docker build ${opts.dockerfile}`,
        result
      );
    }
    return false;
  }

  opts.onEvent?.(opts.suite ?? 'e2e', {
    type: 'build_step',
    message: `${opts.label} base image built`,
  });
  return true;
}

export async function buildImage(opts: DockerBuildOptions): Promise<boolean> {
  const args = ['build', '-f', opts.dockerfile, '-t', opts.tag, '.'];

  const result = await exec({
    cmd: containerRuntime(),
    args,
    cwd: projectRoot(),
    onLine: opts.verbose
      ? (line) => {
          opts.onEvent?.(opts.suite ?? 'e2e', { type: 'output_line', line });
        }
      : undefined,
  });

  if (result.exitCode !== 0 && !opts.verbose) {
    emitBuildFailure(opts.onEvent, opts.suite ?? 'e2e', `docker build ${opts.dockerfile}`, result);
  }

  return result.exitCode === 0;
}

export async function runContainer(
  opts: DockerRunOptions
): Promise<{ exitCode: number; output: string }> {
  const args = ['run', '--rm'];

  if (opts.env) {
    for (const [key, value] of Object.entries(opts.env)) {
      args.push('-e', `${key}=${value}`);
    }
  }

  if (opts.volumes) {
    for (const vol of opts.volumes) {
      args.push('-v', vol);
    }
  }

  args.push(opts.tag);

  const outputChunks: string[] = [];
  const result = await exec({
    cmd: containerRuntime(),
    args,
    cwd: projectRoot(),
    onLine: (line) => {
      outputChunks.push(line);
      opts.onLine?.(line);
    },
  });

  return {
    exitCode: result.exitCode,
    output: outputChunks.join('\n'),
  };
}

export async function removeImage(tag: string): Promise<void> {
  await exec({ cmd: containerRuntime(), args: ['rmi', tag], cwd: projectRoot() });
}

/**
 * Remove orphaned containers and ephemeral images left behind by interrupted
 * test runs (e.g. SIGKILL). Matches the `schmux-{suite}-{pid}` naming pattern
 * and skips the `-base` images which are cached intentionally.
 */
export async function cleanupOrphans(suite: 'scenarios' | 'e2e'): Promise<number> {
  const prefix = `schmux-${suite}-`;
  let cleaned = 0;

  // Kill and remove running/stopped containers from ephemeral images
  const ps = await exec({
    cmd: containerRuntime(),
    args: ['ps', '-a', '--format', '{{.ID}} {{.Image}}'],
    cwd: projectRoot(),
  });
  if (ps.exitCode === 0) {
    for (const line of ps.stdout.trim().split('\n')) {
      if (!line) continue;
      const [id, image] = line.split(' ', 2);
      if (image?.startsWith(prefix)) {
        await exec({ cmd: containerRuntime(), args: ['rm', '-f', id], cwd: projectRoot() });
        cleaned++;
      }
    }
  }

  // Remove ephemeral images (schmux-scenarios-12345, not schmux-scenarios-base)
  const images = await exec({
    cmd: containerRuntime(),
    args: ['images', '--format', '{{.Repository}}'],
    cwd: projectRoot(),
  });
  if (images.exitCode === 0) {
    for (const repo of images.stdout.trim().split('\n')) {
      if (repo.startsWith(prefix) && !repo.endsWith('-base')) {
        await exec({ cmd: containerRuntime(), args: ['rmi', repo], cwd: projectRoot() });
        cleaned++;
      }
    }
  }

  return cleaned;
}
