import type { Options, SuiteName } from './types.js';
import { checkDependencies } from './deps.js';
import { runSuites, setupSignalHandlers } from './runner.js';
import {
  printHeader,
  printSummary,
  printFlakyReport,
  printFinalBanner,
  printCoverageReport,
  printFrontendCoverageReport,
  printDualCoverageReport,
} from './ui.js';
import { compareGoCoverage, compareFrontendCoverage } from './coverage.js';
import { projectRoot } from './exec.js';

function parseArgs(argv: string[]): Options {
  const opts: Options = {
    suites: [],
    all: false,
    race: false,
    verbose: false,
    coverage: false,
    force: false,
    noCache: false,
    quick: false,
    runPattern: null,
    repeat: 1,
    serial: false,
    recordVideo: false,
  };

  let explicitSuite = false;

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    switch (arg) {
      case '--backend':
        explicitSuite = true;
        if (!opts.suites.includes('backend')) opts.suites.push('backend');
        break;
      case '--e2e':
        explicitSuite = true;
        if (!opts.suites.includes('e2e')) opts.suites.push('e2e');
        break;
      case '--scenarios':
        explicitSuite = true;
        if (!opts.suites.includes('scenarios')) opts.suites.push('scenarios');
        break;
      case '--frontend':
        explicitSuite = true;
        if (!opts.suites.includes('frontend')) opts.suites.push('frontend');
        break;
      case '--bench':
        explicitSuite = true;
        if (!opts.suites.includes('bench')) opts.suites.push('bench');
        break;
      case '--all':
        opts.all = true;
        opts.suites = ['backend', 'frontend', 'e2e', 'scenarios'];
        explicitSuite = true;
        break;
      case '--race':
        opts.race = true;
        break;
      case '--verbose':
        opts.verbose = true;
        break;
      case '--coverage':
        opts.coverage = true;
        break;
      case '--quick':
        opts.race = false;
        opts.coverage = false;
        opts.quick = true;
        break;
      case '--force':
        opts.force = true;
        break;
      case '--no-cache':
        opts.noCache = true;
        break;
      case '--serial':
        opts.serial = true;
        break;
      case '--record-video':
        opts.recordVideo = true;
        break;
      case '--run': {
        const pattern = argv[++i];
        if (!pattern) {
          console.error('--run requires a test pattern argument');
          process.exit(1);
        }
        opts.runPattern = pattern;
        break;
      }
      case '--repeat': {
        const n = argv[++i];
        if (!n || isNaN(parseInt(n, 10)) || parseInt(n, 10) < 1) {
          console.error('--repeat requires a positive integer argument');
          process.exit(1);
        }
        opts.repeat = parseInt(n, 10);
        break;
      }
      case '--help':
        printHelp();
        process.exit(0);
        break;
      default:
        console.error(`Unknown option: ${arg}`);
        console.error("Run './test.sh --help' for usage information");
        process.exit(1);
    }
  }

  // Default: all suites (unless --quick or --run narrows it)
  if (!explicitSuite) {
    if (opts.quick) {
      opts.suites = ['backend', 'frontend'];
    } else if (opts.runPattern) {
      opts.suites = ['backend'];
    } else {
      opts.all = true;
      opts.suites = ['backend', 'frontend', 'e2e', 'scenarios'];
    }
  }

  return opts;
}

function printHelp(): void {
  console.log('Usage: ./test.sh [OPTIONS]');
  console.log('');
  console.log('Options:');
  console.log('  --backend       Run backend tests only');
  console.log('  --e2e           Run E2E tests only');
  console.log('  --scenarios     Run scenario tests only (Playwright)');
  console.log('  --frontend      Run frontend tests only');
  console.log('  --bench         Run latency benchmarks only (requires tmux)');
  console.log('  --all           Run all test suites in parallel (same as default, explicit)');
  console.log('  --race          Run with race detector');
  console.log('  --verbose       Run with verbose output');
  console.log('  --coverage      Run with coverage report');
  console.log('  --quick         Run only fast tests (backend + frontend, no Docker)');
  console.log('  --force         Force rebuild Docker base images (skip cache)');
  console.log('  --no-cache      Invalidate Go test cache (force re-run)');
  console.log(
    '  --run PATTERN   Run only tests matching PATTERN (go test -run / playwright --grep)'
  );
  console.log('  --repeat N      Run each test N times and report flaky tests');
  console.log(
    '  --serial        Force serial repeats (one container, --repeat-each) instead of parallel'
  );
  console.log('  --record-video  Keep scenario test videos for all tests, not just failures');
  console.log('  --help          Show this help message');
  console.log('');
  console.log('Examples:');
  console.log(
    '  ./test.sh                                    # Run all tests (backend, frontend, e2e, scenarios)'
  );
  console.log(
    '  ./test.sh --quick                            # Run fast tests only (backend + frontend)'
  );
  console.log('  ./test.sh --all                              # Same as default — run all tests');
  console.log(
    '  ./test.sh --race --verbose                   # All tests with race detector and verbose'
  );
  console.log('  ./test.sh --e2e                              # E2E tests only');
  console.log('  ./test.sh --e2e --run TestE2EOverlayCompounding  # Single E2E test');
  console.log('  ./test.sh --coverage                         # Backend tests with coverage');
  console.log('  ./test.sh --scenarios                        # Scenario tests only (Playwright)');
  console.log("  ./test.sh --scenarios --run 'dispose'        # Scenario tests matching 'dispose'");
  console.log('  ./test.sh --frontend                         # Frontend tests only');
  console.log('  ./test.sh --e2e --force                      # Rebuild base image and run E2E');
  console.log('  ./test.sh --bench                            # Latency benchmarks');
  console.log(
    '  ./test.sh --backend --repeat 5               # Run backend tests 5x, report flaky'
  );
}

async function main(): Promise<void> {
  const opts = parseArgs(process.argv.slice(2));

  setupSignalHandlers();
  await checkDependencies(opts.suites);

  printHeader();

  const { results, flakyResults } = await runSuites(opts);

  printSummary(results, opts.suites.length > 1, opts.repeat);

  // Print coverage reports if available
  for (const r of results) {
    if (r.coverageReport) {
      printCoverageReport(r.coverageReport, r.suite);
    }
    if (r.frontendCoverageReport) {
      printFrontendCoverageReport(r.frontendCoverageReport);
    }
  }

  // Print dual coverage comparison (unit vs integration) when both data sources exist
  if (opts.coverage) {
    const root = projectRoot();
    const hasUnitCoverage = results.some((r) => r.coverageReport);
    const hasIntegration = results.some((r) => r.suite === 'e2e' || r.suite === 'scenarios');

    if (hasUnitCoverage && hasIntegration) {
      const integDirs: string[] = [];
      if (results.some((r) => r.suite === 'e2e')) {
        integDirs.push('build/covdata-e2e');
      }
      if (results.some((r) => r.suite === 'scenarios')) {
        integDirs.push('build/covdata-scenarios');
      }

      const dualReport = await compareGoCoverage('coverage.out', integDirs, root);
      if (dualReport) {
        printDualCoverageReport(dualReport, 'Go');
      }
    }

    // Frontend dual coverage: compare Vitest JSON vs Playwright Istanbul JSON
    const hasFrontendUnit = results.some((r) => r.frontendCoverageReport);
    const hasScenarios = results.some((r) => r.suite === 'scenarios');

    if (hasFrontendUnit && hasScenarios) {
      const { resolve } = await import('node:path');
      const unitCoverageFile = resolve(root, 'assets/dashboard/coverage/coverage-final.json');
      const integCoverageDir = resolve(root, 'test/scenarios/artifacts/fe-coverage');

      const frontendDualReport = compareFrontendCoverage(unitCoverageFile, integCoverageDir);
      if (frontendDualReport) {
        printDualCoverageReport(frontendDualReport, 'Frontend');
      }
    }
  }

  if (opts.repeat > 1) {
    printFlakyReport(flakyResults, opts.repeat);
  }

  const allPassed = results.every((r) => r.status === 'passed');
  const hasBroken = results.some((r) => r.status === 'broken');
  printFinalBanner(allPassed, hasBroken);

  process.exit(allPassed ? 0 : 1);
}

main().catch((err) => {
  console.error('Fatal error:', err);
  process.exit(1);
});
