import { exec } from './exec.js';
import { readFileSync, readdirSync, statSync, type Dirent } from 'node:fs';
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
