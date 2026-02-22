import { readFile, writeFile, rm, mkdir } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import type { RestartManifest, BuildStatus, DevState } from '../types.js';

const SCHMUX_DIR = join(process.env.HOME ?? '~', '.schmux');

export const paths = {
  devState: join(SCHMUX_DIR, 'dev-state.json'),
  devRestart: join(SCHMUX_DIR, 'dev-restart.json'),
  buildStatus: join(SCHMUX_DIR, 'dev-build-status.json'),
  devLog: join(SCHMUX_DIR, 'dev.log'),
  daemonPid: join(SCHMUX_DIR, 'daemon.pid'),
} as const;

export async function ensureSchmuxDir(): Promise<void> {
  if (!existsSync(SCHMUX_DIR)) {
    await mkdir(SCHMUX_DIR, { recursive: true });
  }
}

export async function readRestartManifest(): Promise<RestartManifest | null> {
  try {
    const raw = await readFile(paths.devRestart, 'utf-8');
    return JSON.parse(raw) as RestartManifest;
  } catch {
    return null;
  }
}

export async function writeDevState(state: DevState): Promise<void> {
  await writeFile(paths.devState, JSON.stringify(state));
}

export async function writeBuildStatus(status: BuildStatus): Promise<void> {
  await writeFile(paths.buildStatus, JSON.stringify(status));
}

export async function readDaemonPid(): Promise<number | null> {
  try {
    const raw = await readFile(paths.daemonPid, 'utf-8');
    const pid = parseInt(raw.trim(), 10);
    return isNaN(pid) ? null : pid;
  } catch {
    return null;
  }
}

export async function cleanupStateFiles(): Promise<void> {
  for (const path of [paths.devState, paths.devRestart, paths.buildStatus]) {
    await rm(path, { force: true });
  }
}
