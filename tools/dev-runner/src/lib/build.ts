import { spawn } from 'node:child_process';
import { writeBuildStatus } from './state.js';

export interface BuildResult {
  success: boolean;
  output: string;
}

export function build(
  workspacePath: string,
  binaryPath: string,
  onLine: (line: string) => void
): Promise<BuildResult> {
  return new Promise((resolve) => {
    const proc = spawn('go', ['build', '-o', binaryPath, './cmd/schmux'], {
      cwd: workspacePath,
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

    proc.on('close', async (code) => {
      if (partial) {
        outputLines.push(partial);
        onLine(partial);
      }

      const success = code === 0;
      const output = outputLines.join('\n');
      const timestamp = new Date().toISOString();

      await writeBuildStatus({
        success,
        workspace_path: workspacePath,
        error: success ? '' : output,
        at: timestamp,
      });

      resolve({ success, output });
    });
  });
}
