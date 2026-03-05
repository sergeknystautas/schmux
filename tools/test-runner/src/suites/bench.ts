import { exec, projectRoot } from '../exec.js';
import type { Options, EventCallback, SuiteResult } from '../types.js';
import { mkdirSync } from 'node:fs';
import { resolve } from 'node:path';

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  onEvent('bench', {
    type: 'suite_status',
    status: 'running',
    message: 'Running latency benchmarks...',
  });

  const root = projectRoot();
  const outputLines: string[] = [];
  let anyFailed = false;

  // Check tmux is available (required for PTY/WS benchmarks)
  const tmuxCheck = await exec({ cmd: 'which', args: ['tmux'] });
  if (tmuxCheck.exitCode !== 0) {
    onEvent('bench', {
      type: 'suite_status',
      status: 'broken',
      message: 'tmux is not installed (required for benchmarks)',
    });
    return makeResult('broken', 0, [], 'tmux is not available');
  }

  // Create bench results directory
  const dateStr =
    new Date().toISOString().slice(0, 10).replace(/-/g, '') +
    '-' +
    new Date().toISOString().slice(11, 19).replace(/:/g, '');
  const benchDir = resolve(root, `bench-results/${dateStr}`);
  mkdirSync(benchDir, { recursive: true });

  const benchEnv = { BENCH_OUTPUT_DIR: benchDir };

  onEvent('bench', { type: 'build_step', message: `Output directory: bench-results/${dateStr}` });

  // 1. Percentile tests
  onEvent('bench', { type: 'build_step', message: 'Running percentile tests...' });
  const percentiles = await exec({
    cmd: 'go',
    args: [
      'test',
      '-tags',
      'bench',
      '-run',
      'TestLatency',
      '-timeout',
      '120s',
      './internal/session/',
      '-v',
    ],
    cwd: root,
    env: benchEnv,
    onLine: (line) => {
      outputLines.push(line);
      onEvent('bench', { type: 'output_line', line });
    },
  });
  if (percentiles.exitCode !== 0) {
    onEvent('bench', { type: 'build_step', message: 'Percentile tests failed' });
    anyFailed = true;
  } else {
    onEvent('bench', { type: 'build_step', message: 'Percentile tests passed' });
  }

  // 2. Go benchmark
  onEvent('bench', { type: 'build_step', message: 'Running Go benchmark...' });
  const goBench = await exec({
    cmd: 'go',
    args: [
      'test',
      '-tags',
      'bench',
      '-run=^$',
      '-bench',
      'BenchmarkSendInput',
      '-benchtime',
      '5s',
      '-timeout',
      '120s',
      './internal/session/',
    ],
    cwd: root,
    env: benchEnv,
    onLine: (line) => {
      outputLines.push(line);
      onEvent('bench', { type: 'output_line', line });
    },
  });
  if (goBench.exitCode !== 0) {
    onEvent('bench', { type: 'build_step', message: 'Go benchmark failed' });
    anyFailed = true;
  } else {
    onEvent('bench', { type: 'build_step', message: 'Go benchmark passed' });
  }

  // 3. WebSocket benchmarks (require a running daemon)
  onEvent('bench', {
    type: 'build_step',
    message: 'Checking for running daemon (WebSocket benchmarks)...',
  });
  const healthCheck = await exec({
    cmd: 'curl',
    args: ['-s', '--max-time', '2', 'http://localhost:7337/api/healthz'],
    cwd: root,
  });

  if (healthCheck.exitCode === 0) {
    onEvent('bench', { type: 'build_step', message: 'Daemon reachable' });

    // Get first workspace ID
    const sessionsResp = await exec({
      cmd: 'curl',
      args: ['-s', 'http://localhost:7337/api/sessions'],
      cwd: root,
    });

    let wsId: string | null = null;
    try {
      const sessions = JSON.parse(sessionsResp.stdout);
      if (Array.isArray(sessions) && sessions.length > 0) {
        wsId = sessions[0]?.id ?? null;
      }
    } catch {
      /* ignore */
    }

    if (!wsId) {
      onEvent('bench', {
        type: 'build_step',
        message: 'No workspaces found — skipping WS benchmarks',
      });
    } else {
      // Spawn idle cat session
      onEvent('bench', { type: 'build_step', message: 'Spawning temporary cat session...' });
      const spawnResp = await exec({
        cmd: 'curl',
        args: [
          '-s',
          '-X',
          'POST',
          'http://localhost:7337/api/spawn',
          '-H',
          'Content-Type: application/json',
          '-d',
          JSON.stringify({ workspace_id: wsId, command: 'cat', nickname: 'ws-bench' }),
        ],
        cwd: root,
      });

      let benchSid: string | null = null;
      try {
        const data = JSON.parse(spawnResp.stdout);
        if (Array.isArray(data) && data.length > 0) {
          benchSid = data[0]?.session_id ?? null;
        }
      } catch {
        /* ignore */
      }

      // Spawn stressed session
      const spawnStressedResp = await exec({
        cmd: 'curl',
        args: [
          '-s',
          '-X',
          'POST',
          'http://localhost:7337/api/spawn',
          '-H',
          'Content-Type: application/json',
          '-d',
          JSON.stringify({
            workspace_id: wsId,
            command: "sh -c 'while true; do seq 1 50; sleep 0.05; done & exec cat'",
            nickname: 'ws-bench-stressed',
          }),
        ],
        cwd: root,
      });

      let benchSidStressed: string | null = null;
      try {
        const data = JSON.parse(spawnStressedResp.stdout);
        if (Array.isArray(data) && data.length > 0) {
          benchSidStressed = data[0]?.session_id ?? null;
        }
      } catch {
        /* ignore */
      }

      if (!benchSid) {
        onEvent('bench', {
          type: 'build_step',
          message: 'Failed to spawn cat session for WS benchmarks',
        });
      } else {
        onEvent('bench', { type: 'build_step', message: `Spawned idle session ${benchSid}` });
        if (benchSidStressed) {
          onEvent('bench', {
            type: 'build_step',
            message: `Spawned stressed session ${benchSidStressed}`,
          });
        }

        // Give sessions time to start
        await new Promise((r) => setTimeout(r, 2000));

        const wsEnv: Record<string, string> = {
          ...benchEnv,
          BENCH_SESSION_ID: benchSid,
        };
        if (benchSidStressed) {
          wsEnv['BENCH_SESSION_ID_STRESSED'] = benchSidStressed;
        }

        // WS percentile tests
        onEvent('bench', { type: 'build_step', message: 'Running WebSocket percentile tests...' });
        const wsPercentiles = await exec({
          cmd: 'go',
          args: [
            'test',
            '-tags',
            'bench',
            '-run',
            'TestWSLatency',
            '-timeout',
            '120s',
            './internal/dashboard/',
            '-v',
          ],
          cwd: root,
          env: wsEnv,
          onLine: (line) => {
            outputLines.push(line);
            onEvent('bench', { type: 'output_line', line });
          },
        });
        if (wsPercentiles.exitCode !== 0) {
          onEvent('bench', { type: 'build_step', message: 'WebSocket percentile tests failed' });
          anyFailed = true;
        } else {
          onEvent('bench', { type: 'build_step', message: 'WebSocket percentile tests passed' });
        }

        // WS Go benchmark
        onEvent('bench', { type: 'build_step', message: 'Running WebSocket Go benchmark...' });
        const wsBench = await exec({
          cmd: 'go',
          args: [
            'test',
            '-tags',
            'bench',
            '-run=^$',
            '-bench',
            'BenchmarkWSEcho',
            '-benchtime',
            '5s',
            '-timeout',
            '120s',
            './internal/dashboard/',
          ],
          cwd: root,
          env: wsEnv,
          onLine: (line) => {
            outputLines.push(line);
            onEvent('bench', { type: 'output_line', line });
          },
        });
        if (wsBench.exitCode !== 0) {
          onEvent('bench', { type: 'build_step', message: 'WebSocket Go benchmark failed' });
          anyFailed = true;
        } else {
          onEvent('bench', { type: 'build_step', message: 'WebSocket Go benchmark passed' });
        }

        // Dispose temporary sessions
        onEvent('bench', { type: 'build_step', message: 'Disposing temporary sessions...' });
        await exec({
          cmd: 'curl',
          args: ['-s', '-X', 'POST', `http://localhost:7337/api/sessions/${benchSid}/dispose`],
          cwd: root,
        });
        if (benchSidStressed) {
          await exec({
            cmd: 'curl',
            args: [
              '-s',
              '-X',
              'POST',
              `http://localhost:7337/api/sessions/${benchSidStressed}/dispose`,
            ],
            cwd: root,
          });
        }
        onEvent('bench', { type: 'build_step', message: 'Cleaned up' });
      }
    }
  } else {
    onEvent('bench', {
      type: 'build_step',
      message: 'Daemon not running — skipping WebSocket benchmarks',
    });
    onEvent('bench', {
      type: 'output_line',
      line: "Start daemon with './schmux start' to include WS benchmarks",
    });
  }

  onEvent('bench', {
    type: 'build_step',
    message: `All results saved to: bench-results/${dateStr}/`,
  });

  const totalDuration = percentiles.durationMs + goBench.durationMs;
  const status = anyFailed ? 'failed' : 'passed';

  onEvent('bench', {
    type: 'suite_status',
    status: status === 'passed' ? 'passed' : 'failed',
    message: status === 'passed' ? 'Benchmarks passed' : 'Benchmarks failed',
  });

  return makeResult(status, totalDuration, [], outputLines.join('\n'));
}

function makeResult(
  status: 'passed' | 'failed' | 'broken',
  durationMs: number,
  failedNames: string[],
  output: string
): SuiteResult {
  return {
    suite: 'bench',
    status,
    durationMs,
    passedTests: [],
    failedTests: failedNames.map((n) => ({
      name: n,
      output: '',
      rerunCommand: './test.sh --bench',
    })),
    skippedTests: [],
    testDurations: {},
    output,
  };
}
