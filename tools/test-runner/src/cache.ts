import { createHash } from 'node:crypto';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  unlinkSync,
  writeFileSync,
} from 'node:fs';
import { join, resolve } from 'node:path';
import { exec, projectRoot } from './exec.js';
import type { Options, SuiteName, SuiteResult } from './types.js';

const CACHE_VERSION = 1;
const CACHE_TTL_DAYS = 7;
const CACHE_DIR = '.test-cache';

export interface CacheEntry {
  version: number;
  timestamp: string;
  cacheKey: string;
  status: 'passed';
  durationMs: number;
  passedTests: string[];
  skippedTests: string[];
  testCount: number;
}

export function isCacheable(suite: SuiteName): boolean {
  return suite === 'e2e' || suite === 'scenarios';
}

export function isCacheDisabled(opts: Options): boolean {
  return (
    opts.noCache ||
    opts.runPattern !== null ||
    opts.repeat > 1 ||
    opts.verbose ||
    opts.coverage ||
    opts.recordVideo ||
    opts.force
  );
}

function sha256(data: string): string {
  return createHash('sha256').update(data).digest('hex');
}

function fileHash(filePath: string): string {
  const abs = resolve(projectRoot(), filePath);
  if (!existsSync(abs)) return 'missing';
  return sha256(readFileSync(abs, 'utf-8'));
}

/** Recursively collect all .ts files under a directory, skipping build artifacts. */
function walkTs(dir: string): string[] {
  const skip = new Set(['node_modules', 'test-results', 'playwright-report', 'artifacts']);
  const results: string[] = [];
  if (!existsSync(dir)) return results;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (skip.has(entry.name)) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      results.push(...walkTs(full));
    } else if (entry.name.endsWith('.ts')) {
      results.push(full);
    }
  }
  return results.sort();
}

/** Parse `git status --porcelain` output for dirty/untracked files matching extensions. */
function parseDirtyFiles(porcelain: string, extensions: string[], prefix?: string): string[] {
  return porcelain
    .split('\n')
    .filter((l) => l.length > 0)
    .map((l) => l.slice(3)) // strip status + space
    .filter((f) => {
      if (prefix && !f.startsWith(prefix)) return false;
      return extensions.some((ext) => f.endsWith(ext));
    })
    .sort();
}

export async function computeCacheKey(suite: SuiteName, opts: Options): Promise<string> {
  const root = projectRoot();
  const components: string[] = [];

  // Common git state
  const [headResult, statusResult] = await Promise.all([
    exec({ cmd: 'git', args: ['rev-parse', 'HEAD'], cwd: root }),
    exec({ cmd: 'git', args: ['status', '--porcelain'], cwd: root }),
  ]);

  components.push(`head:${headResult.stdout.trim()}`);

  // Go dependency files
  components.push(`go.mod:${fileHash('go.mod')}`);
  components.push(`go.sum:${fileHash('go.sum')}`);

  // Dirty .go files
  const dirtyGo = parseDirtyFiles(statusResult.stdout, ['.go']);
  for (const f of dirtyGo) {
    components.push(`dirty:${f}:${fileHash(f)}`);
  }

  // E2E dockerfiles
  components.push(`Dockerfile.e2e:${fileHash('Dockerfile.e2e')}`);
  components.push(`Dockerfile.e2e-base:${fileHash('Dockerfile.e2e-base')}`);

  // Build flags
  components.push(`race:${opts.race}`);
  components.push(`coverage:${opts.coverage}`);

  if (suite === 'scenarios') {
    // Frontend deps
    components.push(`package-lock:${fileHash('assets/dashboard/package-lock.json')}`);

    // Dirty frontend files
    const dirtyFe = parseDirtyFiles(
      statusResult.stdout,
      ['.ts', '.tsx', '.css', '.json'],
      'assets/dashboard/'
    );
    for (const f of dirtyFe) {
      components.push(`dirty:${f}:${fileHash(f)}`);
    }

    // Scenario dockerfiles
    components.push(`Dockerfile.scenarios:${fileHash('Dockerfile.scenarios')}`);
    components.push(`Dockerfile.scenarios-base:${fileHash('Dockerfile.scenarios-base')}`);

    // All scenario .ts files
    const scenarioDir = join(root, 'test/scenarios');
    const tsFiles = walkTs(scenarioDir);
    for (const abs of tsFiles) {
      const rel = abs.slice(root.length + 1); // relative path
      components.push(`scenario:${rel}:${sha256(readFileSync(abs, 'utf-8'))}`);
    }

    // Scenario generated config files
    const generatedFiles = [
      'test/scenarios/generated/package.json',
      'test/scenarios/generated/package-lock.json',
      'test/scenarios/generated/tsconfig.json',
      'test/scenarios/generated/entrypoint.sh',
    ];
    for (const f of generatedFiles) {
      components.push(`generated:${f}:${fileHash(f)}`);
    }
  }

  components.sort();
  return sha256(components.join('\n'));
}

function cachePath(suite: SuiteName): string {
  return join(projectRoot(), CACHE_DIR, `${suite}.json`);
}

function ensureCacheDir(): void {
  const dir = join(projectRoot(), CACHE_DIR);
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
}

export function checkCache(
  suite: SuiteName,
  key: string
): { hit: boolean; entry?: CacheEntry; missReason: string } {
  const path = cachePath(suite);

  if (!existsSync(path)) {
    return { hit: false, missReason: 'no previous cache' };
  }

  let entry: CacheEntry;
  try {
    entry = JSON.parse(readFileSync(path, 'utf-8'));
  } catch {
    unlinkSync(path);
    return { hit: false, missReason: 'corrupt cache file' };
  }

  if (entry.version !== CACHE_VERSION) {
    unlinkSync(path);
    return {
      hit: false,
      missReason: `version mismatch: got ${entry.version}, expected ${CACHE_VERSION}`,
    };
  }

  const ageMs = Date.now() - new Date(entry.timestamp).getTime();
  const ageDays = ageMs / (1000 * 60 * 60 * 24);
  if (ageDays > CACHE_TTL_DAYS) {
    unlinkSync(path);
    return {
      hit: false,
      missReason: `cache expired, ${Math.floor(ageDays)}d old`,
    };
  }

  if (entry.cacheKey !== key) {
    return { hit: false, missReason: 'inputs changed' };
  }

  return { hit: true, entry, missReason: '' };
}

export function saveCache(suite: SuiteName, key: string, result: SuiteResult): void {
  ensureCacheDir();

  const entry: CacheEntry = {
    version: CACHE_VERSION,
    timestamp: new Date().toISOString(),
    cacheKey: key,
    status: 'passed',
    durationMs: result.durationMs,
    passedTests: result.passedTests,
    skippedTests: result.skippedTests,
    testCount: result.passedTests.length + result.skippedTests.length,
  };

  const path = cachePath(suite);
  const tmpPath = `${path}.tmp`;
  writeFileSync(tmpPath, JSON.stringify(entry, null, 2));
  renameSync(tmpPath, path);
}

export function deleteCache(suite: SuiteName): void {
  const path = cachePath(suite);
  if (existsSync(path)) {
    unlinkSync(path);
  }
}

export function wipeCacheDir(): void {
  const dir = join(projectRoot(), CACHE_DIR);
  if (existsSync(dir)) {
    rmSync(dir, { recursive: true });
  }
}
