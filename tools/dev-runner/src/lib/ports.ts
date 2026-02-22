import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);

export async function killPort(port: number): Promise<void> {
  let pids: string;
  try {
    const result = await execFileAsync('lsof', ['-ti', `:${port}`]);
    pids = result.stdout.trim();
  } catch {
    return; // No processes on this port
  }

  if (!pids) return;

  const pidList = pids.split('\n').filter(Boolean);
  for (const pid of pidList) {
    try {
      process.kill(parseInt(pid, 10), 'SIGTERM');
    } catch {
      // Already dead
    }
  }

  // Wait for port to be freed (up to 3 seconds)
  for (let i = 0; i < 30; i++) {
    try {
      await execFileAsync('lsof', ['-ti', `:${port}`]);
      await new Promise((r) => setTimeout(r, 100));
    } catch {
      return; // Port is free
    }
  }

  // Force kill remaining
  try {
    const result = await execFileAsync('lsof', ['-ti', `:${port}`]);
    const remaining = result.stdout.trim().split('\n').filter(Boolean);
    for (const pid of remaining) {
      try {
        process.kill(parseInt(pid, 10), 'SIGKILL');
      } catch {
        // Already dead
      }
    }
  } catch {
    // Port is free
  }
}
