import { exec } from './exec.js';
import { containerRuntime } from './docker.js';
import type { SuiteName } from './types.js';

interface DepSpec {
  command: string;
  label: string;
  requiredFor: SuiteName[];
}

const DEPS: DepSpec[] = [
  {
    command: 'go',
    label: 'Go',
    requiredFor: ['backend', 'e2e', 'scenarios', 'bench', 'microbench'],
  },
  {
    command: containerRuntime(),
    label: containerRuntime() === 'podman' ? 'Podman' : 'Docker',
    requiredFor: ['e2e', 'scenarios'],
  },
  { command: 'node', label: 'Node.js', requiredFor: ['frontend'] },
  { command: 'npm', label: 'npm', requiredFor: ['frontend'] },
];

export async function checkDependencies(suites: SuiteName[]): Promise<void> {
  const missing: string[] = [];

  for (const dep of DEPS) {
    const needed = dep.requiredFor.some((s) => suites.includes(s));
    if (!needed) continue;

    const result = await exec({ cmd: 'which', args: [dep.command] });
    if (result.exitCode !== 0) {
      missing.push(`${dep.label} (${dep.command})`);
    }
  }

  if (missing.length > 0) {
    console.error(`\nMissing dependencies: ${missing.join(', ')}`);
    console.error('Install them and re-run.\n');
    process.exit(1);
  }

  // Check for the optional docker buildx plugin. The suites build with
  // `docker build` (the legacy builder), so buildx only speeds things up.
  // Podman doesn't need buildx — it uses buildah natively.
  const needsDocker = suites.some((s) => s === 'e2e' || s === 'scenarios');
  if (needsDocker && containerRuntime() === 'docker') {
    await ensureBuildx();
  }
}

// ensureBuildx checks for the optional docker buildx plugin and, when it is
// absent, falls back to the legacy builder. The suites build with `docker build`
// (not `docker buildx build`), so buildx is never required. Absence is common:
// buildx may not be installed, or ~/.docker may be fenced so the plugin can't be
// discovered. In those cases warn and continue rather than prompting or writing
// under the home dir — both of which break inside a fence or any non-interactive
// run.
async function ensureBuildx(): Promise<void> {
  const check = await exec({ cmd: 'docker', args: ['buildx', 'version'] });
  if (check.exitCode === 0) return;

  console.error('\n  docker buildx not found; using the legacy builder (slower).');
  console.error(
    '  Install it outside any fence for faster builds: https://docs.docker.com/go/buildx/\n'
  );
}
