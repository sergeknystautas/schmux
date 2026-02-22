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
render(<App devRoot={devRoot} />);
