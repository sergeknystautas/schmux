import { exec, projectRoot } from '../exec.js';
import type { Options, EventCallback, SuiteResult } from '../types.js';

// Micro-benchmarks: fast, isolated Go benchmarks for hot-path functions.
// No tmux, no daemon, no external dependencies. Just `go test -bench`.
const microBenchPackages = [
  './internal/escbuf/',
  './internal/dashboard/',
  './internal/remote/controlmode/',
  './internal/session/',
];

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  onEvent('microbench', {
    type: 'suite_status',
    status: 'running',
    message: 'Running micro-benchmarks...',
  });

  const root = projectRoot();
  const outputLines: string[] = [];
  let anyFailed = false;

  const args = [
    'test',
    '-run=^$', // skip unit tests, only benchmarks
    '-bench=.',
    '-benchmem',
    '-count=5',
    '-timeout=120s',
    ...microBenchPackages,
  ];

  onEvent('microbench', {
    type: 'build_step',
    message: `go ${args.join(' ')}`,
  });

  const result = await exec({
    cmd: 'go',
    args,
    cwd: root,
    onLine: (line) => {
      outputLines.push(line);
      onEvent('microbench', { type: 'output_line', line });
    },
  });

  if (result.exitCode !== 0) {
    anyFailed = true;
    onEvent('microbench', {
      type: 'build_step',
      message: 'Micro-benchmarks failed',
    });
  } else {
    onEvent('microbench', {
      type: 'build_step',
      message: 'Micro-benchmarks passed (output is benchstat-compatible with -count=5)',
    });
  }

  const status = anyFailed ? 'failed' : 'passed';
  onEvent('microbench', {
    type: 'suite_status',
    status: status === 'passed' ? 'passed' : 'failed',
    message: status === 'passed' ? 'Micro-benchmarks passed' : 'Micro-benchmarks failed',
  });

  return {
    suite: 'microbench',
    status,
    durationMs: result.durationMs,
    passedTests: [],
    failedTests: anyFailed
      ? [
          {
            name: 'micro-benchmarks',
            output: outputLines.join('\n'),
            rerunCommand: './test.sh --microbench',
          },
        ]
      : [],
    skippedTests: [],
    testDurations: {},
    output: outputLines.join('\n'),
  };
}
