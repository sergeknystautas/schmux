import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { existsSync } from 'node:fs';
import { performance } from 'node:perf_hooks';

export interface ExecOptions {
  cmd: string;
  args: string[];
  cwd?: string;
  env?: Record<string, string>;
  onLine?: (line: string) => void;
  verbose?: boolean;
}

export interface ExecResult {
  exitCode: number;
  stdout: string;
  stderr: string;
  durationMs: number;
}

export async function exec(opts: ExecOptions): Promise<ExecResult> {
  const start = performance.now();

  return new Promise<ExecResult>((res) => {
    const proc = spawn(opts.cmd, opts.args, {
      cwd: opts.cwd ?? projectRoot(),
      stdio: ['ignore', 'pipe', 'pipe'],
      env: opts.env ? { ...process.env, ...opts.env } : process.env,
    });

    const stdoutChunks: string[] = [];
    const stderrChunks: string[] = [];
    let stdoutPartial = '';
    let stderrPartial = '';

    proc.stdout.on('data', (data: Buffer) => {
      const text = data.toString();
      stdoutChunks.push(text);

      if (opts.onLine) {
        stdoutPartial += text;
        const lines = stdoutPartial.split('\n');
        stdoutPartial = lines.pop()!;
        for (const line of lines) {
          opts.onLine(line);
        }
      }
    });

    proc.stderr.on('data', (data: Buffer) => {
      const text = data.toString();
      stderrChunks.push(text);

      if (opts.onLine) {
        stderrPartial += text;
        const lines = stderrPartial.split('\n');
        stderrPartial = lines.pop()!;
        for (const line of lines) {
          opts.onLine(line);
        }
      }
    });

    proc.on('close', (code) => {
      // Flush remaining partial lines
      if (opts.onLine && stdoutPartial) opts.onLine(stdoutPartial);
      if (opts.onLine && stderrPartial) opts.onLine(stderrPartial);

      res({
        exitCode: code ?? 1,
        stdout: stdoutChunks.join(''),
        stderr: stderrChunks.join(''),
        durationMs: performance.now() - start,
      });
    });

    proc.on('error', () => {
      res({
        exitCode: 1,
        stdout: stdoutChunks.join(''),
        stderr: stderrChunks.join(''),
        durationMs: performance.now() - start,
      });
    });
  });
}

let cachedRoot: string | null = null;

export function projectRoot(): string {
  if (cachedRoot) return cachedRoot;

  const thisFile = fileURLToPath(import.meta.url);
  let dir = dirname(thisFile);

  while (dir !== '/') {
    if (existsSync(resolve(dir, 'go.mod'))) {
      cachedRoot = dir;
      return dir;
    }
    dir = dirname(dir);
  }

  throw new Error('Could not find project root (no go.mod found)');
}
