import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);

interface DepSpec {
  command: string;
  label: string;
}

const DEPS: DepSpec[] = [
  { command: 'go', label: 'Go' },
  { command: 'node', label: 'Node.js' },
  { command: 'tmux', label: 'tmux' },
];

export async function checkDependencies(): Promise<string[]> {
  const missing: string[] = [];
  for (const dep of DEPS) {
    try {
      await execFileAsync('which', [dep.command]);
    } catch {
      missing.push(`${dep.label} (${dep.command})`);
    }
  }
  return missing;
}
