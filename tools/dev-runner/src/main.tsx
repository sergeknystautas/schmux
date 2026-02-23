import React from 'react';
import { render } from 'ink';
import { App } from './App.js';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { existsSync } from 'node:fs';

function projectRoot(): string {
  const thisFile = fileURLToPath(import.meta.url);
  let dir = dirname(thisFile);
  while (dir !== '/') {
    if (existsSync(resolve(dir, 'go.mod'))) {
      return dir;
    }
    dir = dirname(dir);
  }
  throw new Error('Could not find project root (no go.mod found)');
}

const devRoot = projectRoot();
const plain = process.argv.includes('--plain');
const stdout = process.stdout;
const originalWrite = stdout.write.bind(stdout) as typeof stdout.write;

let cleaned = false;
function cleanup() {
  if (cleaned) return;
  cleaned = true;
  stdout.write = originalWrite;
  if (!plain) {
    originalWrite('\x1b[?25h\x1b[?1049l'); // show cursor, leave alt screen
  }
}

if (!plain) {
  // Enter alternate screen buffer, hide cursor, clear screen
  originalWrite('\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H');

  // Micro-batch stdout writes and wrap with synchronized output markers (DEC 2026).
  // Ink may call stdout.write multiple times per render cycle (cursor moves, clear,
  // content). setImmediate collects all writes from one synchronous render cycle and
  // flushes them as a single atomic draw, eliminating flicker on supporting terminals.
  let writeBuf = '';
  let flushPending = false;

  stdout.write = function (chunk: any, encodingOrCb?: any, cb?: any) {
    writeBuf += typeof chunk === 'string' ? chunk : chunk.toString();
    if (!flushPending) {
      flushPending = true;
      setImmediate(() => {
        const data = writeBuf;
        writeBuf = '';
        flushPending = false;
        originalWrite(`\x1b[?2026h${data}\x1b[?2026l`);
      });
    }
    const callback = typeof encodingOrCb === 'function' ? encodingOrCb : cb;
    if (callback) setImmediate(callback);
    return true;
  } as any;
}

process.on('exit', cleanup);

const instance = render(<App devRoot={devRoot} plain={plain} />);
instance.waitUntilExit().then(() => {
  cleanup();
  process.exit(0);
});
