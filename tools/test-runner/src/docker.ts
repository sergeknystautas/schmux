import { exec, projectRoot } from './exec.js';
import type { EventCallback, SuiteName } from './types.js';

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

export async function isDockerAvailable(): Promise<boolean> {
  const result = await exec({ cmd: 'docker', args: ['info'], cwd: projectRoot() });
  return result.exitCode === 0;
}

export async function imageExists(tag: string): Promise<boolean> {
  const result = await exec({
    cmd: 'docker',
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
    cmd: 'docker',
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
    cmd: 'docker',
    args,
    cwd: projectRoot(),
    onLine: opts.verbose
      ? (line) => {
          opts.onEvent?.(opts.suite ?? 'e2e', { type: 'output_line', line });
        }
      : undefined,
  });

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
    cmd: 'docker',
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
  await exec({ cmd: 'docker', args: ['rmi', tag], cwd: projectRoot() });
}
