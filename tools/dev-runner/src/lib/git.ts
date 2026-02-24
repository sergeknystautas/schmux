import { spawn } from 'node:child_process';

export interface GitPullResult {
  success: boolean;
  output: string;
}

export function gitPull(cwd: string, onLine: (line: string) => void): Promise<GitPullResult> {
  return new Promise((resolve) => {
    const proc = spawn('git', ['pull'], {
      cwd,
      stdio: ['ignore', 'pipe', 'pipe'],
      env: process.env,
    });

    const outputLines: string[] = [];
    let partial = '';

    const handleData = (data: Buffer) => {
      partial += data.toString();
      const lines = partial.split('\n');
      partial = lines.pop()!;
      for (const line of lines) {
        outputLines.push(line);
        onLine(line);
      }
    };

    proc.stdout.on('data', handleData);
    proc.stderr.on('data', handleData);

    proc.on('error', (err) => {
      resolve({ success: false, output: `Failed to spawn git: ${err.message}` });
    });

    proc.on('close', (code) => {
      if (partial) {
        outputLines.push(partial);
        onLine(partial);
      }
      resolve({ success: code === 0, output: outputLines.join('\n') });
    });
  });
}
