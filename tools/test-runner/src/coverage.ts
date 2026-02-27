import { exec } from './exec.js';
import { readFileSync, readdirSync, statSync, existsSync, type Dirent } from 'node:fs';
import { resolve, join } from 'node:path';

// ─── Types ──────────────────────────────────────────────────────────────────

export interface PackageCoverage {
  name: string; // e.g. "internal/dashboard" or "lib"
  avgCoverage: number; // 0-100
  funcCount: number;
  uncoveredCount: number;
  loc: number;
}

export interface FuncCoverage {
  pkg: string;
  file: string;
  line: number;
  funcName: string;
  coverage: number;
}

export interface CoverageReport {
  totalCoverage: number;
  packages: PackageCoverage[];
  uncoveredFunctions: FuncCoverage[]; // all 0% functions
}

// ─── Go Coverage Analysis ───────────────────────────────────────────────────

export async function analyzeGoCoverage(
  coverageFile: string,
  root: string
): Promise<CoverageReport> {
  // Run go tool cover -func to get per-function coverage
  const coverResult = await exec({
    cmd: 'go',
    args: ['tool', 'cover', `-func=${coverageFile}`],
    cwd: root,
  });

  if (coverResult.exitCode !== 0) {
    return { totalCoverage: 0, packages: [], uncoveredFunctions: [] };
  }

  // Parse module path from go.mod
  const goMod = readFileSync(resolve(root, 'go.mod'), 'utf-8');
  const moduleLine = goMod.match(/^module\s+(\S+)/m);
  const modulePrefix = moduleLine ? moduleLine[1] + '/' : '';

  // Parse each line of go tool cover output
  // Format: github.com/.../pkg/file.go:NN:   funcName    XX.X%
  const lines = coverResult.stdout.trim().split('\n');
  const funcEntries: FuncCoverage[] = [];
  let totalCoverage = 0;

  // Per-package accumulators
  const pkgMap = new Map<
    string,
    { coverageSum: number; funcCount: number; uncoveredCount: number }
  >();

  for (const line of lines) {
    // Last line is "total: (statements)  XX.X%"
    const totalMatch = line.match(/^total:\s+\(statements\)\s+(\d+\.\d+)%/);
    if (totalMatch) {
      totalCoverage = parseFloat(totalMatch[1]);
      continue;
    }

    // Regular line: path/file.go:NN:  funcName  XX.X%
    const match = line.match(/^(\S+):(\d+):\s+(\S+)\s+(\d+\.\d+)%/);
    if (!match) continue;

    const fullPath = match[1];
    const lineNum = parseInt(match[2], 10);
    const funcName = match[3];
    const coverage = parseFloat(match[4]);

    // Strip module prefix and filename to get package path
    let relPath = fullPath;
    if (modulePrefix && relPath.startsWith(modulePrefix)) {
      relPath = relPath.slice(modulePrefix.length);
    }
    // Extract package dir (everything before the last /)
    const lastSlash = relPath.lastIndexOf('/');
    const pkg = lastSlash > 0 ? relPath.substring(0, lastSlash) : relPath;
    const fileName = lastSlash > 0 ? relPath.substring(lastSlash + 1) : relPath;

    // Accumulate per-package stats
    let pkgStats = pkgMap.get(pkg);
    if (!pkgStats) {
      pkgStats = { coverageSum: 0, funcCount: 0, uncoveredCount: 0 };
      pkgMap.set(pkg, pkgStats);
    }
    pkgStats.coverageSum += coverage;
    pkgStats.funcCount++;
    if (coverage === 0) {
      pkgStats.uncoveredCount++;
      funcEntries.push({ pkg, file: fileName, line: lineNum, funcName, coverage });
    }
  }

  // Count LoC per package
  const locByPkg = countGoLoc(root, modulePrefix);

  // Build package list
  const packages: PackageCoverage[] = [];
  for (const [name, stats] of pkgMap) {
    packages.push({
      name,
      avgCoverage: stats.funcCount > 0 ? stats.coverageSum / stats.funcCount : 0,
      funcCount: stats.funcCount,
      uncoveredCount: stats.uncoveredCount,
      loc: locByPkg.get(name) ?? 0,
    });
  }

  // Sort by coverage ascending
  packages.sort((a, b) => a.avgCoverage - b.avgCoverage);

  return { totalCoverage, packages, uncoveredFunctions: funcEntries };
}

// ─── LoC Counting ───────────────────────────────────────────────────────────

function countGoLoc(root: string, modulePrefix: string): Map<string, number> {
  const locByPkg = new Map<string, number>();

  // Walk Go source directories, count non-blank lines in .go files (excluding _test.go)
  function walk(dir: string, pkgPath: string): void {
    let entries: Dirent[];
    try {
      entries = readdirSync(dir, { withFileTypes: true, encoding: 'utf-8' }) as Dirent[];
    } catch {
      return;
    }

    let pkgLoc = 0;
    let hasGoFiles = false;

    for (const entry of entries) {
      const fullPath = join(dir, entry.name);

      if (entry.isDirectory()) {
        // Skip vendor, .git, testdata, node_modules, assets
        if (
          entry.name === 'vendor' ||
          entry.name === '.git' ||
          entry.name === 'testdata' ||
          entry.name === 'node_modules' ||
          entry.name === 'assets'
        ) {
          continue;
        }
        const childPkg = pkgPath ? `${pkgPath}/${entry.name}` : entry.name;
        walk(fullPath, childPkg);
      } else if (entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
        hasGoFiles = true;
        try {
          const content = readFileSync(fullPath, 'utf-8');
          const lines = content.split('\n');
          for (const line of lines) {
            if (line.trim().length > 0) {
              pkgLoc++;
            }
          }
        } catch {
          // Skip unreadable files
        }
      }
    }

    if (hasGoFiles && pkgPath) {
      locByPkg.set(pkgPath, (locByPkg.get(pkgPath) ?? 0) + pkgLoc);
    }
  }

  // Walk from project root, looking at directories that match Go package structure
  // Go packages in this project live under cmd/ and internal/
  for (const topDir of ['cmd', 'internal', 'pkg']) {
    const topPath = join(root, topDir);
    try {
      statSync(topPath);
      walk(topPath, topDir);
    } catch {
      // Directory doesn't exist
    }
  }

  // Also check root-level .go files (package main at root)
  try {
    const rootEntries = readdirSync(root, { withFileTypes: true });
    let rootLoc = 0;
    for (const entry of rootEntries) {
      if (entry.isFile() && entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
        const content = readFileSync(join(root, entry.name), 'utf-8');
        for (const line of content.split('\n')) {
          if (line.trim().length > 0) rootLoc++;
        }
      }
    }
    if (rootLoc > 0) {
      locByPkg.set('.', rootLoc);
    }
  } catch {
    // Skip
  }

  return locByPkg;
}

// ─── Vitest Coverage Parsing ────────────────────────────────────────────────

// Strip ANSI escape codes
function stripAnsi(str: string): string {
  return str.replace(/\x1b\[[0-9;]*m/g, '');
}

export interface FrontendPackageCoverage {
  name: string; // directory name
  stmtsCoverage: number;
  funcsCoverage: number;
}

export interface FrontendCoverageReport {
  totalCoverage: number;
  directories: FrontendPackageCoverage[];
}

export function parseVitestCoverage(output: string): FrontendCoverageReport | null {
  const lines = output.split('\n').map(stripAnsi);

  // Find the istanbul text coverage table
  // Header format: " File          | % Stmts | % Branch | % Funcs | % Lines | Uncovered Line #s "
  const headerIdx = lines.findIndex(
    (l) => l.includes('% Stmts') && l.includes('% Funcs') && l.includes('% Lines')
  );
  if (headerIdx < 0) return null;

  const directories: FrontendPackageCoverage[] = [];
  let totalCoverage = 0;

  // Parse rows after the header (skip the separator line after header)
  for (let i = headerIdx + 2; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line || line.startsWith('---') || line.startsWith('===')) continue;

    // End of table
    if (line.match(/^-+$/)) continue;

    // Parse table row: " name  | XX.XX | XX.XX | XX.XX | XX.XX | ... "
    const parts = line.split('|').map((p) => p.trim());
    if (parts.length < 5) continue;

    const name = parts[0];
    const stmts = parseFloat(parts[1]);
    const funcs = parseFloat(parts[3]);

    if (isNaN(stmts)) continue;

    // "All files" is the total row
    if (name === 'All files') {
      totalCoverage = stmts;
      continue;
    }

    // Skip individual files (they have extensions), keep directories only
    if (name.includes('.')) continue;

    // Skip empty names
    if (!name) continue;

    directories.push({
      name,
      stmtsCoverage: stmts,
      funcsCoverage: isNaN(funcs) ? 0 : funcs,
    });
  }

  // Sort by stmts coverage ascending
  directories.sort((a, b) => a.stmtsCoverage - b.stmtsCoverage);

  return { totalCoverage, directories };
}

// ─── Dual Coverage Comparison (Unit vs Integration) ─────────────────────────

export interface DualCoveragePackage {
  name: string;
  unitOnly: number; // stmts covered only by unit tests
  integOnly: number; // stmts covered only by integration tests
  both: number; // stmts covered by both
  neither: number; // stmts covered by neither
  total: number; // total stmts
}

export interface DualCoverageReport {
  packages: DualCoveragePackage[];
  totals: DualCoveragePackage;
}

/**
 * Parse a Go coverprofile text file into a map of statement blocks.
 * Each block is keyed by "file:startLine.startCol,endLine.endCol".
 * Value is { pkg, numStmts, count }.
 *
 * Format: mode: atomic
 *         github.com/.../file.go:10.2,15.3 2 1
 *         file:startLine.startCol,endLine.endCol numStatements count
 */
export function parseCoverProfile(
  text: string,
  modulePrefix: string
): Map<string, { pkg: string; numStmts: number; count: number }> {
  const blocks = new Map<string, { pkg: string; numStmts: number; count: number }>();

  for (const line of text.split('\n')) {
    if (!line || line.startsWith('mode:')) continue;

    // Format: path/file.go:startLine.startCol,endLine.endCol numStmts count
    const match = line.match(/^(\S+):(\d+\.\d+),(\d+\.\d+)\s+(\d+)\s+(\d+)$/);
    if (!match) continue;

    const fullPath = match[1];
    const start = match[2];
    const end = match[3];
    const numStmts = parseInt(match[4], 10);
    const count = parseInt(match[5], 10);

    // Strip module prefix to get relative path
    let relPath = fullPath;
    if (modulePrefix && relPath.startsWith(modulePrefix)) {
      relPath = relPath.slice(modulePrefix.length);
    }

    // Extract package dir (everything before the last /)
    const lastSlash = relPath.lastIndexOf('/');
    const pkg = lastSlash > 0 ? relPath.substring(0, lastSlash) : relPath;

    const key = `${fullPath}:${start},${end}`;

    // Merge: if same block appears multiple times, sum the counts
    const existing = blocks.get(key);
    if (existing) {
      existing.count += count;
    } else {
      blocks.set(key, { pkg, numStmts, count });
    }
  }

  return blocks;
}

/**
 * Convert binary covdata directories to a text coverprofile file using
 * `go tool covdata textfmt`. Returns the path to the output file, or null
 * if conversion fails or no data exists.
 */
export async function convertCovdataToProfile(
  covdataDirs: string[],
  outputFile: string,
  root: string
): Promise<string | null> {
  // Filter to directories that actually exist and contain files
  const validDirs = covdataDirs.filter((d) => {
    try {
      if (!existsSync(d)) return false;
      const entries = readdirSync(d);
      return entries.length > 0;
    } catch {
      return false;
    }
  });

  if (validDirs.length === 0) return null;

  const result = await exec({
    cmd: 'go',
    args: ['tool', 'covdata', 'textfmt', `-i=${validDirs.join(',')}`, `-o=${outputFile}`],
    cwd: root,
  });

  if (result.exitCode !== 0) return null;

  return outputFile;
}

/**
 * Compare unit test coverage against integration test coverage.
 * Returns per-package breakdown of which statements are covered by
 * unit only, integration only, both, or neither.
 */
export async function compareGoCoverage(
  unitProfileFile: string,
  integrationDirs: string[],
  root: string
): Promise<DualCoverageReport | null> {
  // Read the unit test coverprofile
  let unitText: string;
  try {
    unitText = readFileSync(resolve(root, unitProfileFile), 'utf-8');
  } catch {
    return null;
  }

  // Convert integration covdata to text profile
  const integProfileFile = resolve(root, 'coverage-integration.out');
  const integProfile = await convertCovdataToProfile(
    integrationDirs.map((d) => resolve(root, d)),
    integProfileFile,
    root
  );

  if (!integProfile) return null;

  let integText: string;
  try {
    integText = readFileSync(integProfile, 'utf-8');
  } catch {
    return null;
  }

  // Parse module prefix
  const goMod = readFileSync(resolve(root, 'go.mod'), 'utf-8');
  const moduleLine = goMod.match(/^module\s+(\S+)/m);
  const modulePrefix = moduleLine ? moduleLine[1] + '/' : '';

  // Parse both profiles
  const unitBlocks = parseCoverProfile(unitText, modulePrefix);
  const integBlocks = parseCoverProfile(integText, modulePrefix);

  // Collect all block keys from both profiles
  const allKeys = new Set([...unitBlocks.keys(), ...integBlocks.keys()]);

  // Per-package accumulators
  const pkgMap = new Map<
    string,
    { unitOnly: number; integOnly: number; both: number; neither: number; total: number }
  >();

  for (const key of allKeys) {
    const unit = unitBlocks.get(key);
    const integ = integBlocks.get(key);

    const pkg = unit?.pkg ?? integ?.pkg ?? 'unknown';
    const numStmts = unit?.numStmts ?? integ?.numStmts ?? 0;
    const unitCovered = (unit?.count ?? 0) > 0;
    const integCovered = (integ?.count ?? 0) > 0;

    let stats = pkgMap.get(pkg);
    if (!stats) {
      stats = { unitOnly: 0, integOnly: 0, both: 0, neither: 0, total: 0 };
      pkgMap.set(pkg, stats);
    }

    stats.total += numStmts;
    if (unitCovered && integCovered) {
      stats.both += numStmts;
    } else if (unitCovered) {
      stats.unitOnly += numStmts;
    } else if (integCovered) {
      stats.integOnly += numStmts;
    } else {
      stats.neither += numStmts;
    }
  }

  // Build sorted package list
  const packages: DualCoveragePackage[] = [];
  for (const [name, stats] of pkgMap) {
    packages.push({ name, ...stats });
  }
  packages.sort((a, b) => a.name.localeCompare(b.name));

  // Compute totals
  const totals: DualCoveragePackage = {
    name: 'Total',
    unitOnly: packages.reduce((s, p) => s + p.unitOnly, 0),
    integOnly: packages.reduce((s, p) => s + p.integOnly, 0),
    both: packages.reduce((s, p) => s + p.both, 0),
    neither: packages.reduce((s, p) => s + p.neither, 0),
    total: packages.reduce((s, p) => s + p.total, 0),
  };

  return { packages, totals };
}

// ─── Frontend Dual Coverage (Istanbul JSON) ─────────────────────────────────

/**
 * Istanbul coverage format: { [filePath]: FileCoverage }
 * FileCoverage has: s (statement counts), f (function counts), b (branch counts)
 */
interface IstanbulFileCoverage {
  path: string;
  s: Record<string, number>; // statement index → execution count
  f: Record<string, number>; // function index → execution count
  b: Record<string, number[]>; // branch index → [truthy count, falsy count]
  statementMap: Record<string, { start: { line: number }; end: { line: number } }>;
}

type IstanbulCoverageMap = Record<string, IstanbulFileCoverage>;

/**
 * Merge multiple Istanbul coverage JSON objects into one.
 * Sums execution counters for matching files/statements.
 */
function mergeIstanbulCoverage(coverages: IstanbulCoverageMap[]): IstanbulCoverageMap {
  const merged: IstanbulCoverageMap = {};

  for (const coverage of coverages) {
    for (const [filePath, fileCov] of Object.entries(coverage)) {
      if (!merged[filePath]) {
        merged[filePath] = JSON.parse(JSON.stringify(fileCov));
        continue;
      }

      const target = merged[filePath];
      // Sum statement counts
      for (const [key, count] of Object.entries(fileCov.s)) {
        target.s[key] = (target.s[key] ?? 0) + count;
      }
      // Sum function counts
      for (const [key, count] of Object.entries(fileCov.f)) {
        target.f[key] = (target.f[key] ?? 0) + count;
      }
      // Sum branch counts (arrays)
      for (const [key, counts] of Object.entries(fileCov.b)) {
        if (!target.b[key]) {
          target.b[key] = [...counts];
        } else {
          for (let i = 0; i < counts.length; i++) {
            target.b[key][i] = (target.b[key][i] ?? 0) + counts[i];
          }
        }
      }
    }
  }

  return merged;
}

/**
 * Load and merge all Istanbul coverage JSON files from a directory.
 * Each file is a per-test coverage snapshot written by the Playwright fixture.
 */
function loadIstanbulCoverageDir(dir: string): IstanbulCoverageMap | null {
  if (!existsSync(dir)) return null;

  let entries: string[];
  try {
    entries = readdirSync(dir).filter((f) => f.endsWith('.json'));
  } catch {
    return null;
  }

  if (entries.length === 0) return null;

  const coverages: IstanbulCoverageMap[] = [];
  for (const entry of entries) {
    try {
      const content = readFileSync(join(dir, entry), 'utf-8');
      coverages.push(JSON.parse(content) as IstanbulCoverageMap);
    } catch {
      // Skip malformed files
    }
  }

  return coverages.length > 0 ? mergeIstanbulCoverage(coverages) : null;
}

/**
 * Extract directory name from a file path relative to the project.
 * E.g., "src/components/Foo.tsx" → "src/components"
 */
function extractFrontendDir(filePath: string): string {
  // Find "src/" in the path and take the next directory level
  const srcIdx = filePath.indexOf('/src/');
  if (srcIdx < 0) return 'other';

  const afterSrc = filePath.substring(srcIdx + 5); // after "/src/"
  const nextSlash = afterSrc.indexOf('/');
  if (nextSlash < 0) return 'src';
  return `src/${afterSrc.substring(0, nextSlash)}`;
}

/**
 * Compare frontend unit (Vitest) coverage against integration (Playwright) coverage.
 * Both use Istanbul JSON format. Returns per-directory breakdown.
 */
export function compareFrontendCoverage(
  unitCoverageFile: string,
  integrationCoverageDir: string
): DualCoverageReport | null {
  // Load unit coverage (Vitest JSON)
  let unitCoverage: IstanbulCoverageMap;
  try {
    unitCoverage = JSON.parse(readFileSync(unitCoverageFile, 'utf-8')) as IstanbulCoverageMap;
  } catch {
    return null;
  }

  // Load and merge integration coverage (per-test Playwright JSONs)
  const integCoverage = loadIstanbulCoverageDir(integrationCoverageDir);
  if (!integCoverage) return null;

  // Collect all file paths from both
  const allFiles = new Set([...Object.keys(unitCoverage), ...Object.keys(integCoverage)]);

  // Per-directory accumulators
  const dirMap = new Map<
    string,
    { unitOnly: number; integOnly: number; both: number; neither: number; total: number }
  >();

  for (const filePath of allFiles) {
    const dir = extractFrontendDir(filePath);
    const unitFile = unitCoverage[filePath];
    const integFile = integCoverage[filePath];

    let stats = dirMap.get(dir);
    if (!stats) {
      stats = { unitOnly: 0, integOnly: 0, both: 0, neither: 0, total: 0 };
      dirMap.set(dir, stats);
    }

    // Compare at statement level
    const unitStmts = unitFile?.s ?? {};
    const integStmts = integFile?.s ?? {};
    const allStmtKeys = new Set([...Object.keys(unitStmts), ...Object.keys(integStmts)]);

    for (const key of allStmtKeys) {
      const unitCovered = (unitStmts[key] ?? 0) > 0;
      const integCovered = (integStmts[key] ?? 0) > 0;

      stats.total++;
      if (unitCovered && integCovered) {
        stats.both++;
      } else if (unitCovered) {
        stats.unitOnly++;
      } else if (integCovered) {
        stats.integOnly++;
      } else {
        stats.neither++;
      }
    }
  }

  // Build sorted directory list
  const packages: DualCoveragePackage[] = [];
  for (const [name, stats] of dirMap) {
    packages.push({ name, ...stats });
  }
  packages.sort((a, b) => a.name.localeCompare(b.name));

  // Compute totals
  const totals: DualCoveragePackage = {
    name: 'Total',
    unitOnly: packages.reduce((s, p) => s + p.unitOnly, 0),
    integOnly: packages.reduce((s, p) => s + p.integOnly, 0),
    both: packages.reduce((s, p) => s + p.both, 0),
    neither: packages.reduce((s, p) => s + p.neither, 0),
    total: packages.reduce((s, p) => s + p.total, 0),
  };

  return { packages, totals };
}
