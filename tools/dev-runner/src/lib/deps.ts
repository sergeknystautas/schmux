import { execFile, spawn } from 'node:child_process';
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

/** Run `npm install` in a directory, streaming output to onLine. */
export function npmInstall(cwd: string, onLine: (line: string) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const proc = spawn('npm', ['install'], {
      cwd,
      stdio: ['ignore', 'pipe', 'pipe'],
      env: process.env,
    });

    let partial = '';
    const handleData = (data: Buffer) => {
      partial += data.toString();
      const lines = partial.split('\n');
      partial = lines.pop()!;
      for (const line of lines) {
        onLine(line);
      }
    };

    proc.stdout.on('data', handleData);
    proc.stderr.on('data', handleData);

    proc.on('close', (code) => {
      if (partial) onLine(partial);
      if (code === 0) resolve();
      else reject(new Error(`npm install exited with code ${code}`));
    });

    proc.on('error', reject);
  });
}
