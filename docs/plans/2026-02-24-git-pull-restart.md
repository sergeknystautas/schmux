# Git Pull and Restart Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Add a `p` keyboard shortcut to the dev runner TUI that runs `git pull` in the project root, then rebuilds the Go binary and restarts both daemon and Vite.

**Architecture:** A new `gitPull()` helper function (modeled after `build()`) runs `git pull` in `devRoot` and streams output. The existing `handleRestart` flow is extended: pull first, then rebuild. The `ProcessStatus` type gains a `'pulling'` variant so the status bar shows the pull phase distinctly.

**Tech Stack:** TypeScript (Ink/React TUI), Node.js `child_process`

---

### Task 1: Add `gitPull` helper function

**Files:**

- Create: `tools/dev-runner/src/lib/git.ts`

**Step 1: Write the git pull helper**

```typescript
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

    proc.on('close', (code) => {
      if (partial) {
        outputLines.push(partial);
        onLine(partial);
      }
      resolve({ success: code === 0, output: outputLines.join('\n') });
    });
  });
}
```

**Step 2: Verify it compiles**

```bash
npx --prefix tools/dev-runner tsc --noEmit
```

Expected: no errors

**Step 3: Commit**

Use `/commit`

---

### Task 2: Add `'pulling'` to `ProcessStatus` and StatusBar

**Files:**

- Modify: `tools/dev-runner/src/types.ts:1`
- Modify: `tools/dev-runner/src/components/StatusBar.tsx:17,33`

**Step 1: Add `'pulling'` to ProcessStatus union**

In `types.ts`, change line 1:

```typescript
export type ProcessStatus =
  | 'idle'
  | 'starting'
  | 'running'
  | 'stopped'
  | 'crashed'
  | 'building'
  | 'pulling';
```

**Step 2: Handle `'pulling'` in StatusBar**

In `StatusBar.tsx`, add `'pulling'` alongside `'building'` in both functions:

In `statusDot` (line 17), change:

```typescript
    case 'starting':
    case 'building':
```

to:

```typescript
    case 'starting':
    case 'building':
    case 'pulling':
```

In `statusLabel` (line 33), add a new case before `'building'`:

```typescript
    case 'pulling':
      return 'pulling';
```

**Step 3: Verify it compiles**

```bash
npx --prefix tools/dev-runner tsc --noEmit
```

Expected: no errors

**Step 4: Commit**

Use `/commit`

---

### Task 3: Add `onPull` to `useKeyboard` hook

**Files:**

- Modify: `tools/dev-runner/src/hooks/useKeyboard.ts`

**Step 1: Add onPull to the interface and input handler**

Change `UseKeyboardOptions` to add `onPull`:

```typescript
interface UseKeyboardOptions {
  onRestart: () => void;
  onPull: () => void;
  onClear: () => void;
  onQuit: () => void;
  onToggleLayout: () => void;
  canRestart: boolean;
}
```

Update the `useKeyboard` function to destructure `onPull` and handle the `p` key:

```typescript
export function useKeyboard({
  onRestart,
  onPull,
  onClear,
  onQuit,
  onToggleLayout,
  canRestart,
}: UseKeyboardOptions): void {
  useInput((input) => {
    if (input === 'r' && canRestart) {
      onRestart();
    } else if (input === 'p' && canRestart) {
      onPull();
    } else if (input === 'c') {
      onClear();
    } else if (input === 'l') {
      onToggleLayout();
    } else if (input === 'q') {
      onQuit();
    }
  });
}
```

Note: `p` reuses `canRestart` — you can't pull while building/pulling.

**Step 2: Verify it compiles (will fail — App.tsx doesn't pass `onPull` yet)**

```bash
npx --prefix tools/dev-runner tsc --noEmit
```

Expected: type error in `App.tsx` — that's fine, fixed in Task 4.

---

### Task 4: Wire up `handlePull` in App.tsx

**Files:**

- Modify: `tools/dev-runner/src/App.tsx`

**Step 1: Add the import**

Add `gitPull` to the imports at the top of `App.tsx`:

```typescript
import { gitPull } from './lib/git.js';
```

**Step 2: Add the `handlePull` callback**

Add this after the existing `handleRestart` callback (after line 309):

```typescript
const handlePull = useCallback(async () => {
  setBackendStatusOverride('pulling');
  addBackendLine('Pulling latest changes...');
  const pullResult = await gitPull(devRoot, addBackendLine);
  if (!pullResult.success) {
    addBackendLine('Pull failed — not restarting');
    setBackendStatusOverride(null);
    return;
  }
  addBackendLine('Pull succeeded, rebuilding...');
  setBackendStatusOverride('building');
  await backend.stop();
  const result = await build(workspaceRef.current, binaryPath, addBackendLine);
  setBackendStatusOverride(null);
  if (result.success) {
    addBackendLine('Build succeeded, restarting daemon...');
    backend.start();
  } else {
    addBackendLine('Build failed');
  }
}, [backend, binaryPath, addBackendLine, devRoot]);
```

**Step 3: Pass `onPull` to `useKeyboard`**

Change the `useKeyboard` call to include `onPull`:

```typescript
useKeyboard({
  onRestart: handleRestart,
  onPull: handlePull,
  onClear: handleClear,
  onQuit: handleQuit,
  onToggleLayout: handleToggleLayout,
  canRestart,
});
```

**Step 4: Verify it compiles**

```bash
npx --prefix tools/dev-runner tsc --noEmit
```

Expected: no errors

**Step 5: Commit**

Use `/commit`

---

### Task 5: Update KeyBar to show `[p] pull` shortcut

**Files:**

- Modify: `tools/dev-runner/src/components/KeyBar.tsx`

**Step 1: Add pull shortcut to both plain and normal modes**

In the plain mode branch (after the `r restart backend` text, before `q quit`), add:

```tsx
          <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
            p
          </Text>
          <Text dimColor={!canRestart}> pull </Text>
```

In the normal mode branch (same position — after `r restart backend`, before `c clear logs`), add:

```tsx
        <Text bold dimColor={!canRestart} color={canRestart ? 'white' : undefined}>
          p
        </Text>
        <Text dimColor={!canRestart}> pull </Text>
```

**Step 2: Verify it compiles**

```bash
npx --prefix tools/dev-runner tsc --noEmit
```

Expected: no errors

**Step 3: Commit**

Use `/commit`
