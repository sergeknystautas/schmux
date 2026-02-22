import { useState, useCallback, useRef, useEffect } from 'react';
import { spawn, type ChildProcess } from 'node:child_process';
import type { ProcessStatus } from '../types.js';

export interface UseProcessOptions {
  command: string;
  args: string[];
  cwd?: string;
  env?: Record<string, string>;
  onLine: (line: string) => void;
  onExit?: (code: number) => void;
}

export interface UseProcessReturn {
  status: ProcessStatus;
  start: () => void;
  stop: () => Promise<void>;
  restart: () => Promise<void>;
}

export function useProcess(opts: UseProcessOptions): UseProcessReturn {
  const [status, setStatus] = useState<ProcessStatus>('idle');
  const procRef = useRef<ChildProcess | null>(null);
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const stop = useCallback(async () => {
    const proc = procRef.current;
    if (!proc || proc.killed) {
      setStatus('stopped');
      return;
    }

    proc.kill('SIGTERM');
    await new Promise<void>((resolve) => {
      const timeout = setTimeout(() => {
        proc.kill('SIGKILL');
        resolve();
      }, 5000);
      proc.on('close', () => {
        clearTimeout(timeout);
        resolve();
      });
    });
    procRef.current = null;
    setStatus('stopped');
  }, []);

  const start = useCallback(() => {
    const o = optsRef.current;
    setStatus('starting');

    const proc = spawn(o.command, o.args, {
      cwd: o.cwd,
      stdio: ['ignore', 'pipe', 'pipe'],
      env: o.env ? { ...process.env, ...o.env } : process.env,
    });

    procRef.current = proc;

    let partial = '';
    const handleData = (data: Buffer) => {
      partial += data.toString();
      const lines = partial.split('\n');
      partial = lines.pop()!;
      for (const line of lines) {
        optsRef.current.onLine(line);
      }
    };

    proc.stdout?.on('data', handleData);
    proc.stderr?.on('data', handleData);

    // Mark as running once first output arrives or after a short delay
    const runningTimer = setTimeout(() => setStatus('running'), 500);
    const markRunning = () => {
      clearTimeout(runningTimer);
      setStatus('running');
    };
    proc.stdout?.once('data', markRunning);

    proc.on('close', (code) => {
      clearTimeout(runningTimer);
      if (partial) {
        optsRef.current.onLine(partial);
        partial = '';
      }
      procRef.current = null;
      setStatus(code === 0 ? 'stopped' : 'crashed');
      optsRef.current.onExit?.(code ?? 1);
    });

    proc.on('error', () => {
      clearTimeout(runningTimer);
      procRef.current = null;
      setStatus('crashed');
    });
  }, []);

  const restart = useCallback(async () => {
    await stop();
    start();
  }, [stop, start]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      const proc = procRef.current;
      if (proc && !proc.killed) {
        proc.kill('SIGTERM');
      }
    };
  }, []);

  return { status, start, stop, restart };
}
