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

  // Ensure Docker BuildKit (buildx) is available for Docker-based suites
  // Podman doesn't need buildx — it uses buildah natively.
  const needsDocker = suites.some((s) => s === 'e2e' || s === 'scenarios');
  if (needsDocker && containerRuntime() === 'docker') {
    await ensureBuildx();
  }
}

async function ensureBuildx(): Promise<void> {
  const check = await exec({ cmd: 'docker', args: ['buildx', 'version'] });
  if (check.exitCode === 0) return;

  const { existsSync, writeFileSync, mkdirSync, symlinkSync } = await import('node:fs');
  const { resolve } = await import('node:path');
  const homeDir = process.env.HOME ?? '~';
  const declineFlag = resolve(homeDir, '.schmux/.buildx-declined');

  if (existsSync(declineFlag)) return;

  // Check if brew is available before prompting
  const brewCheck = await exec({ cmd: 'which', args: ['brew'] });
  if (brewCheck.exitCode !== 0) {
    console.error('\n  docker buildx is not installed. Docker builds will be slower.');
    console.error('  Install it manually: https://docs.docker.com/go/buildx/\n');
    return;
  }

  // Ask the user
  const { createInterface } = await import('node:readline');
  const rl = createInterface({ input: process.stdin, output: process.stdout });
  const answer = await new Promise<string>((r) => {
    rl.question('  Docker BuildKit (buildx) not found. Install via brew? [Y/n] ', r);
  });
  rl.close();

  if (answer.trim().toLowerCase() === 'n') {
    console.error('  Skipping buildx install. Docker builds will be slower.');
    mkdirSync(resolve(homeDir, '.schmux'), { recursive: true });
    writeFileSync(declineFlag, '');
    return;
  }

  const install = await exec({ cmd: 'brew', args: ['install', 'docker-buildx'] });
  if (install.exitCode !== 0) {
    console.error('\n  Failed to install docker-buildx via brew.');
    console.error('  Install it manually: https://docs.docker.com/go/buildx/\n');
    process.exit(1);
  }

  // Find the installed binary and link it as a Docker CLI plugin
  const prefix = await exec({ cmd: 'brew', args: ['--prefix', 'docker-buildx'] });
  const buildxBin = prefix.stdout.trim() + '/bin/docker-buildx';

  const pluginDir = resolve(homeDir, '.docker/cli-plugins');
  const pluginPath = resolve(pluginDir, 'docker-buildx');

  if (!existsSync(pluginPath)) {
    mkdirSync(pluginDir, { recursive: true });
    symlinkSync(buildxBin, pluginPath);
  }

  // Verify it works
  const verify = await exec({ cmd: 'docker', args: ['buildx', 'version'] });
  if (verify.exitCode !== 0) {
    console.error('\n  docker buildx installed but not working.');
    console.error(
      '  Try manually: mkdir -p ~/.docker/cli-plugins && ln -sfn ' +
        buildxBin +
        ' ' +
        pluginPath +
        '\n'
    );
    process.exit(1);
  }

  console.log('  Docker BuildKit installed successfully.');
}
