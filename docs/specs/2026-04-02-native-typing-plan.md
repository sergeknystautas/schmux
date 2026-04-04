# Plan: Native Typing — Client-Side Input Buffering

**Goal**: Eliminate perceived typing latency over high-latency connections by buffering printable keystrokes client-side, echoing them locally in xterm.js, and flushing to the server on Enter.
**Architecture**: Primarily client-side (terminalStream.ts) with one server-side fix (keyclassify.go UTF-8 support). Per-session toggle persisted in localStorage. Cursor repositioning on flush ensures server echo overwrites local echo without duplication.
**Tech Stack**: TypeScript (frontend), Go (server-side UTF-8 fix), xterm.js
**Design**: `docs/specs/2026-04-02-native-typing-design.md`

**Changes from v1**: Fixed backspace wrap boundary logic (use absolute CUP instead of double relative `\x1b[A`), added `bootstrapComplete` guard on handleOutput flush, clarified paste handler ordering in sendInput, fixed test setup to match existing patterns, split Step 9 into separate steps, added echoLocally unit test.

---

## Dependency Groups

| Group | Steps       | Can Parallelize | Notes                                                                        |
| ----- | ----------- | --------------- | ---------------------------------------------------------------------------- |
| 1     | Steps 1-2   | Yes             | Server-side UTF-8 fix + frontend buffer state (independent)                  |
| 2     | Step 3      | No              | Input classification (depends on buffer state from Step 2)                   |
| 3     | Steps 4-5   | No              | Local echo + flush (depends on classification from Step 3)                   |
| 4     | Step 6      | No              | Backspace handling (depends on buffer state from Step 4)                     |
| 5     | Steps 7-8   | Yes             | Sync guard + inputLatency guard (independent, both depend on buffer state)   |
| 6     | Step 9      | No              | Paste handling (depends on flush from Step 5)                                |
| 7     | Steps 10-12 | Yes             | Edge cases: resize, scroll, server output (independent, all depend on flush) |
| 8     | Step 13     | No              | UI toggle in SessionDetailPage (depends on terminalStream support)           |
| 9     | Step 14     | No              | End-to-end verification                                                      |

---

## Step 1: Fix ClassifyKeyRuns to handle multi-byte UTF-8

**File**: `internal/remote/controlmode/keyclassify.go`

The existing classifier treats only bytes 32-126 as printable. Multi-byte UTF-8 characters (>= 0x80) fall through and are silently dropped. Native typing sends batched strings that may contain Unicode.

### 1a. Write failing test

**File**: `internal/remote/controlmode/keyclassify_test.go` (new file)

```go
package controlmode

import "testing"

func TestClassifyKeyRuns_UTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRuns []KeyRun
	}{
		{
			name:  "ASCII only",
			input: "hello",
			wantRuns: []KeyRun{
				{Text: "hello", Literal: true},
			},
		},
		{
			name:  "accented characters",
			input: "café",
			wantRuns: []KeyRun{
				{Text: "café", Literal: true},
			},
		},
		{
			name:  "emoji",
			input: "hello 🚀 world",
			wantRuns: []KeyRun{
				{Text: "hello 🚀 world", Literal: true},
			},
		},
		{
			name:  "CJK characters",
			input: "你好世界",
			wantRuns: []KeyRun{
				{Text: "你好世界", Literal: true},
			},
		},
		{
			name:  "mixed UTF-8 and special keys",
			input: "café\r",
			wantRuns: []KeyRun{
				{Text: "café", Literal: true},
				{Text: "Enter", Literal: false},
			},
		},
		{
			name:  "UTF-8 between control characters",
			input: "\tcafé\t",
			wantRuns: []KeyRun{
				{Text: "Tab", Literal: false},
				{Text: "café", Literal: true},
				{Text: "Tab", Literal: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyKeyRuns(nil, tt.input)
			if len(got) != len(tt.wantRuns) {
				t.Fatalf("got %d runs, want %d: %+v", len(got), len(tt.wantRuns), got)
			}
			for i, want := range tt.wantRuns {
				if got[i].Text != want.Text || got[i].Literal != want.Literal {
					t.Errorf("run[%d] = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/remote/controlmode/ -run TestClassifyKeyRuns_UTF8 -v
```

### 1c. Write implementation

**File**: `internal/remote/controlmode/keyclassify.go`

Change the printable character loop (line 40) to include UTF-8 continuation bytes:

```go
// Find run of printable characters (ASCII 32-126 or UTF-8 multi-byte)
j := i
for j < len(keys) {
	b := keys[j]
	if b >= 32 && b < 127 {
		// ASCII printable
		j++
	} else if b >= 0xC0 && b <= 0xF7 {
		// UTF-8 leading byte — consume the full character
		// 0xC0-0xDF: 2-byte, 0xE0-0xEF: 3-byte, 0xF0-0xF7: 4-byte
		size := 2
		if b >= 0xE0 && b <= 0xEF {
			size = 3
		} else if b >= 0xF0 {
			size = 4
		}
		// Verify we have enough bytes and they're valid continuation bytes
		if j+size <= len(keys) {
			valid := true
			for k := 1; k < size; k++ {
				if keys[j+k] < 0x80 || keys[j+k] > 0xBF {
					valid = false
					break
				}
			}
			if valid {
				j += size
				continue
			}
		}
		break // Invalid UTF-8, stop the run
	} else {
		break // Control char or other non-printable
	}
}
```

### 1d. Run test to verify it passes

```bash
go test ./internal/remote/controlmode/ -run TestClassifyKeyRuns_UTF8 -v
```

### 1e. Commit

```bash
git add internal/remote/controlmode/keyclassify.go internal/remote/controlmode/keyclassify_test.go
git commit -m "fix(controlmode): handle multi-byte UTF-8 in ClassifyKeyRuns

The printable character loop only recognized ASCII 32-126. Multi-byte
UTF-8 characters (accented, CJK, emoji) had bytes >= 0x80 which fell
through to the default case and were silently dropped. This is
prerequisite for native typing which sends batched Unicode strings."
```

---

## Step 2: Add local buffer state to TerminalStream

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Add the buffer state fields and the `nativeTypingEnabled` flag. No behavioral changes yet — just state and the public API to enable/disable.

### 2a. Write failing test

**File**: `assets/dashboard/src/lib/terminalStream.test.ts` (add to existing test file)

Use the existing test file's container setup pattern (`beforeEach` with container element creation, `await stream.initialized`):

```typescript
describe('native typing buffer state', () => {
  let container: HTMLDivElement;

  beforeEach(() => {
    container = document.createElement('div');
    // Mock getBoundingClientRect for terminal dimension calculation
    container.getBoundingClientRect = () => ({
      width: 800,
      height: 600,
      x: 0,
      y: 0,
      top: 0,
      right: 800,
      bottom: 600,
      left: 0,
      toJSON: () => {},
    });
    document.body.appendChild(container);
  });

  afterEach(() => {
    document.body.removeChild(container);
  });

  it('should initialize with native typing disabled and empty buffer', async () => {
    const stream = new TerminalStream('test-session', container, {});
    await stream.initialized;
    expect(stream.nativeTypingEnabled).toBe(false);
    expect(stream.localBuffer).toBe('');
    expect(stream.localEchoStart).toBeNull();
    stream.dispose();
  });

  it('should toggle native typing on and off', async () => {
    const stream = new TerminalStream('test-session', container, {});
    await stream.initialized;
    stream.setNativeTyping(true);
    expect(stream.nativeTypingEnabled).toBe(true);
    stream.setNativeTyping(false);
    expect(stream.nativeTypingEnabled).toBe(false);
    stream.dispose();
  });
});
```

### 2b. Run test to verify it fails

```bash
./test.sh --quick
```

### 2c. Write implementation

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Add to class fields (around line 100, near other state declarations):

```typescript
// Native typing: client-side input buffering for low-latency typing
nativeTypingEnabled = false;
localBuffer = '';
localEchoStart: { row: number; col: number } | null = null;
```

Add public method:

```typescript
setNativeTyping(enabled: boolean) {
  this.nativeTypingEnabled = enabled;
  if (!enabled && this.localBuffer.length > 0) {
    this.flushLocalBuffer();
  }
}
```

Add stub for `flushLocalBuffer` (implemented in Step 5):

```typescript
private flushLocalBuffer(trailingKey?: string) {
  // Stub — implemented in Step 5
  if (this.localBuffer.length > 0) {
    this.sendRawInput(this.localBuffer);
  }
  if (trailingKey) {
    this.sendRawInput(trailingKey);
  }
  this.localBuffer = '';
  this.localEchoStart = null;
}
```

### 2d. Run test to verify it passes

```bash
./test.sh --quick
```

### 2e. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "feat(native-typing): add buffer state and toggle to TerminalStream"
```

---

## Step 3: Input classification — route keystrokes through native typing

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Modify `sendInput()` to classify keystrokes as buffered or immediate when native typing is enabled.

### 3a. Write failing test

```typescript
describe('native typing input classification', () => {
  it('should identify printable ASCII as buffered', () => {
    expect(isBufferedInput('a')).toBe(true);
    expect(isBufferedInput('Z')).toBe(true);
    expect(isBufferedInput(' ')).toBe(true);
    expect(isBufferedInput('5')).toBe(true);
    expect(isBufferedInput('!')).toBe(true);
  });

  it('should identify unicode as buffered', () => {
    expect(isBufferedInput('é')).toBe(true);
    expect(isBufferedInput('你')).toBe(true);
    expect(isBufferedInput('🚀')).toBe(true);
  });

  it('should identify backspace as buffered', () => {
    expect(isBufferedInput('\x7f')).toBe(true);
    expect(isBufferedInput('\x08')).toBe(true);
  });

  it('should identify control characters as immediate', () => {
    expect(isBufferedInput('\r')).toBe(false); // Enter
    expect(isBufferedInput('\t')).toBe(false); // Tab
    expect(isBufferedInput('\x03')).toBe(false); // Ctrl+C
    expect(isBufferedInput('\x1b')).toBe(false); // Escape
    expect(isBufferedInput('\x1b[A')).toBe(false); // Up arrow
    expect(isBufferedInput('\x1b\r')).toBe(false); // Alt+Enter
    expect(isBufferedInput('\x1b\x7f')).toBe(false); // Alt+Backspace
  });
});
```

### 3b. Run test to verify it fails

```bash
./test.sh --quick
```

### 3c. Write implementation

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Add the classification function (module-level, not on the class):

```typescript
/**
 * Determine if a keystroke should be buffered for native typing.
 * Buffered: printable characters (including unicode/emoji) and backspace.
 * Immediate: everything else (Enter, Tab, Ctrl+*, arrows, Escape, etc.)
 */
function isBufferedInput(data: string): boolean {
  if (data.length === 0) return false;

  // Backspace
  if (data === '\x7f' || data === '\x08') return true;

  // Multi-character sequences are control (arrows, function keys, Alt+*)
  // Exception: multi-code-unit unicode chars (emoji) are a single character
  // but may be multiple UTF-16 code units. Check first code point.
  const codePoint = data.codePointAt(0);
  if (codePoint === undefined) return false;

  // Single printable character (or multi-code-unit unicode like emoji)
  // A single printable character has length 1 or 2 (surrogate pair).
  const charLen = codePoint > 0xffff ? 2 : 1;
  if (data.length === charLen && codePoint >= 32) return true;

  return false;
}
```

Modify `sendInput()` to use classification. **Important ordering**: paste handling (multi-char) comes first, then single-char classification, then immediate fallthrough:

```typescript
sendInput(data: string) {
  // Ctrl+V clipboard image handling (existing)
  if (data === '\x16') {
    // ... existing clipboard logic unchanged ...
    return;
  }

  // Native typing: classify and route
  if (this.nativeTypingEnabled && this.followTail) {
    // Paste or multi-character input: process character by character.
    // This MUST come before the single-char isBufferedInput check,
    // because a paste string like "hello" (length 5) would fail
    // isBufferedInput (which expects single chars) and fall through
    // to the immediate path, sending the whole paste as raw input
    // without local echo.
    if (data.length > 1) {
      const codePoint = data.codePointAt(0);
      const singleCharLen = codePoint && codePoint > 0xFFFF ? 2 : 1;
      // Only enter paste mode for true multi-character input,
      // not single emoji (which have length 2 due to surrogate pairs)
      if (data.length > singleCharLen) {
        const chars = Array.from(data); // Handle surrogate pairs
        for (const char of chars) {
          if (isBufferedInput(char)) {
            if (char === '\x7f' || char === '\x08') {
              this.localBackspace();
            } else {
              this.echoLocally(char);
            }
          } else {
            // Non-printable in paste: flush buffer, send this char immediately
            if (this.localBuffer.length > 0) {
              this.flushLocalBuffer(char);
            } else {
              this.sendRawInput(char);
            }
          }
        }
        return;
      }
    }

    // Single character (or single emoji): classify normally
    if (isBufferedInput(data)) {
      if (data === '\x7f' || data === '\x08') {
        this.localBackspace();
      } else {
        this.echoLocally(data);
      }
      return;
    }
    // Immediate key: flush buffer first, then send
    if (this.localBuffer.length > 0) {
      this.flushLocalBuffer(data);
      return;
    }
  }

  this.sendRawInput(data);
}
```

### 3d. Run test to verify it passes

```bash
./test.sh --quick
```

### 3e. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "feat(native-typing): input classification for buffered vs immediate keys"
```

---

## Step 4: Local echo — write characters into xterm.js buffer

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Implement `echoLocally()` — save cursor position on first character, write to terminal.

### 4a. Write test

**File**: `assets/dashboard/src/lib/terminalStream.test.ts`

The existing test file has mocked terminal instances that track `write` calls. Test that `echoLocally` updates buffer state correctly:

```typescript
describe('native typing echoLocally', () => {
  let container: HTMLDivElement;

  beforeEach(() => {
    container = document.createElement('div');
    container.getBoundingClientRect = () => ({
      width: 800,
      height: 600,
      x: 0,
      y: 0,
      top: 0,
      right: 800,
      bottom: 600,
      left: 0,
      toJSON: () => {},
    });
    document.body.appendChild(container);
  });

  afterEach(() => {
    document.body.removeChild(container);
  });

  it('should save cursor position on first character and accumulate buffer', async () => {
    const stream = new TerminalStream('test-session', container, {});
    await stream.initialized;
    stream.setNativeTyping(true);

    // Simulate typing — sendInput routes to echoLocally when native typing is on
    stream.sendInput('h');
    expect(stream.localBuffer).toBe('h');
    expect(stream.localEchoStart).not.toBeNull();

    stream.sendInput('i');
    expect(stream.localBuffer).toBe('hi');

    stream.dispose();
  });
});
```

### 4b. Write implementation

Add method to `TerminalStream`:

```typescript
private echoLocally(char: string) {
  if (!this.terminal) return;

  // Save cursor position on first buffered character
  if (this.localEchoStart === null) {
    const buf = this.terminal.buffer.active;
    this.localEchoStart = {
      row: buf.cursorY,
      col: buf.cursorX,
    };
  }

  this.localBuffer += char;
  this.terminal.write(char);
}
```

### 4c. Run tests

```bash
./test.sh --quick
```

### 4d. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "feat(native-typing): local echo rendering into xterm.js buffer"
```

---

## Step 5: Flush and cursor reconciliation

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Replace the stub `flushLocalBuffer()` with the real implementation: cursor rewind + send + clear.

### 5a. Write implementation

```typescript
private flushLocalBuffer(trailingKey?: string) {
  if (!this.terminal) return;

  // Step 1: Cursor rewind — move xterm.js cursor back to where typing started.
  // CUP escape uses 1-indexed, viewport-relative coordinates.
  if (this.localEchoStart !== null) {
    const row = this.localEchoStart.row + 1; // 0-indexed -> 1-indexed
    const col = this.localEchoStart.col + 1;
    this.terminal.write(`\x1b[${row};${col}H`);
  }

  // Step 2: Send buffered text + optional trailing key to server.
  if (this.localBuffer.length > 0) {
    this.sendRawInput(this.localBuffer);
  }
  if (trailingKey) {
    this.sendRawInput(trailingKey);
  }

  // Step 3: Clear local state.
  this.localBuffer = '';
  this.localEchoStart = null;
}
```

### 5b. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "feat(native-typing): flush with cursor rewind for echo reconciliation"
```

---

## Step 6: Backspace handling

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Implement `localBackspace()` — erase last character from buffer and screen, handling line wrap boundaries and wide characters.

### 6a. Write implementation

```typescript
private localBackspace() {
  if (!this.terminal || this.localBuffer.length === 0) return;

  // Determine the character being removed and its display width.
  // Use Array.from to handle surrogate pairs (emoji) correctly.
  const chars = Array.from(this.localBuffer);
  const removedChar = chars[chars.length - 1];
  chars.pop();
  this.localBuffer = chars.join('');

  // Get display width via xterm.js unicode handler.
  // wcwidth: 1 for standard chars, 2 for wide (CJK, some emoji).
  const width = this.terminal.unicode.getStringCellWidth(removedChar);

  const cursorX = this.terminal.buffer.active.cursorX;

  if (cursorX >= width) {
    // Simple case: cursor is not at wrap boundary.
    // Move back, overwrite with spaces, move back.
    const back = '\b'.repeat(width);
    const spaces = ' '.repeat(width);
    this.terminal.write(back + spaces + back);
  } else {
    // Wrap boundary: cursor is at column 0 (or column 1 for wide char).
    // Use absolute CUP positioning to avoid xterm.js deferred-wrap issues.
    // Deferred wrap means writing at the last column does NOT immediately
    // wrap — the cursor stays with a pending-wrap flag. Relative movement
    // (\x1b[A) after that would move from the wrong row.
    const buf = this.terminal.buffer.active;
    const cursorY = buf.cursorY; // viewport-relative, 0-indexed
    const cols = this.terminal.cols;
    const targetRow = cursorY;   // one row up (0-indexed, will +1 for CUP)
    const targetCol = cols - width + 1; // 1-indexed for CUP
    this.terminal.write(
      `\x1b[${targetRow};${targetCol}H` + // CUP to target cell (1-indexed row = cursorY, since we want one row up from cursorY+1 conceptually, but cursorY is 0-indexed so cursorY as 1-indexed = one row up)
      ' '.repeat(width) +                 // Overwrite with spaces
      `\x1b[${targetRow};${targetCol}H`   // Reposition back
    );
  }

  // If buffer is now empty, clear the start position.
  if (this.localBuffer.length === 0) {
    this.localEchoStart = null;
  }
}
```

### 6b. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "feat(native-typing): backspace with line-wrap and wide character support"
```

---

## Step 7: Sync guard — skip sync while buffer is non-empty

**File**: `assets/dashboard/src/lib/terminalStream.ts`

When the sync mechanism fires and the local buffer is non-empty, it would detect locally-echoed text as a mismatch and erase it. Add a guard.

### 7a. Write implementation

In `handleSync()` (line ~1358), add an early return at the top:

```typescript
private handleSync(msg: { ... }) {
  if (!this.terminal) return;

  // Native typing guard: don't compare while local echo is active —
  // the locally-echoed text would be detected as a mismatch.
  if (this.localBuffer.length > 0) {
    this.tsLog('sync.skipped', { reason: 'nativeTypingActive' });
    this.sendSyncResult(false, []);
    return;
  }

  // ... existing handleSync logic unchanged ...
}
```

### 7b. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "fix(native-typing): skip sync comparison while local buffer is non-empty"
```

---

## Step 8: Disable inputLatency.markSent during native typing

**File**: `assets/dashboard/src/lib/terminalStream.ts`

The latency tracker assumes 1:1 keystroke-to-echo pairing. With batched input, measurements are meaningless. Skip `markSent()` when native typing is active.

### 8a. Write implementation

Modify `sendRawInput()` (line ~1054):

```typescript
private sendRawInput(data: string) {
  if (this.ws && this.ws.readyState === WebSocket.OPEN) {
    if (!this.nativeTypingEnabled) {
      inputLatency.markSent();
    }
    this.ws.send(new TextEncoder().encode(data));
  }
}
```

### 8b. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "fix(native-typing): skip inputLatency.markSent during batched input"
```

---

## Step 9: Paste handling in sendInput

**File**: `assets/dashboard/src/lib/terminalStream.ts`

Paste handling is already integrated into `sendInput` in Step 3c (the multi-char branch comes before single-char classification). No additional code needed — this step verifies the paste path works correctly.

### 9a. Write test

```typescript
describe('native typing paste handling', () => {
  it('should process paste character by character', async () => {
    const stream = new TerminalStream('test-session', container, {});
    await stream.initialized;
    stream.setNativeTyping(true);

    // Paste "hi" — should buffer both characters
    stream.sendInput('hi');
    expect(stream.localBuffer).toBe('hi');

    stream.dispose();
  });

  it('should flush on Enter within paste', async () => {
    const stream = new TerminalStream('test-session', container, {});
    await stream.initialized;
    stream.setNativeTyping(true);

    // Paste "hello\nworld" — should flush "hello" on the newline
    stream.sendInput('hello\rworld');
    // After processing: "hello" flushed, "world" in buffer
    expect(stream.localBuffer).toBe('world');

    stream.dispose();
  });
});
```

### 9b. Run tests

```bash
./test.sh --quick
```

### 9c. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "test(native-typing): paste handling verification"
```

---

## Step 10: Flush on resize

**File**: `assets/dashboard/src/lib/terminalStream.ts`

In `fitTerminal()` (the resize handler), add at the top:

```typescript
// Native typing: flush buffer before resize invalidates cursor position
if (this.localBuffer.length > 0) {
  this.flushLocalBuffer();
}
```

### 10a. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "fix(native-typing): flush local buffer on terminal resize"
```

---

## Step 11: Flush on scroll away

**File**: `assets/dashboard/src/lib/terminalStream.ts`

In the wheel event handler (where `followTail` is set to false on upward scroll):

```typescript
if (e.deltaY < 0 && this.followTail) {
  // Native typing: flush before disabling follow mode
  if (this.localBuffer.length > 0) {
    this.flushLocalBuffer();
  }
  this.followTail = false;
  // ... existing logic ...
}
```

### 11a. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "fix(native-typing): flush local buffer on scroll away"
```

---

## Step 12: Flush on server output

**File**: `assets/dashboard/src/lib/terminalStream.ts`

In `handleOutput()`, when a binary frame arrives during active local echo, flush the buffer. Guard with `bootstrapComplete` to avoid flushing during initial connection bootstrap.

```typescript
handleOutput(data: string | ArrayBuffer) {
  // Native typing: flush local buffer when server output arrives
  // to prevent cursor position invalidation.
  // Guard with bootstrapComplete to avoid flushing during initial bootstrap
  // (the first binary frame triggers terminal.reset + full replay).
  if (this.localBuffer.length > 0 && data instanceof ArrayBuffer && this.bootstrapComplete) {
    this.flushLocalBuffer();
  }

  // ... existing handleOutput logic unchanged ...
}
```

### 12a. Commit

```bash
git add assets/dashboard/src/lib/terminalStream.ts
git commit -m "fix(native-typing): flush local buffer on server output with bootstrap guard"
```

---

## Step 13: UI toggle in SessionDetailPage

**File**: `assets/dashboard/src/routes/SessionDetailPage.tsx`

Add a per-session toggle in the terminal toolbar, next to the "Select lines" button. Persisted in localStorage keyed by session ID.

### 13a. Write implementation

Add state and localStorage persistence (near line 60):

```typescript
const [nativeTyping, setNativeTyping] = useLocalStorage<boolean>(
  `nativeTyping:${sessionId}`,
  false
);
```

Wire the toggle to TerminalStream (in the effect that creates the terminal, after `terminalStreamRef.current` is set):

```typescript
// Sync native typing state to terminal stream
useEffect(() => {
  terminalStreamRef.current?.setNativeTyping(nativeTyping);
}, [nativeTyping]);
```

Add the toggle button in the toolbar (after the `selectionMode` conditional block at line ~849, before the "Download log" button at line ~851):

```tsx
<Tooltip
  content={
    nativeTyping
      ? 'Disable native typing (buffered input)'
      : 'Enable native typing (reduces input latency)'
  }
>
  <button
    className={`btn btn--sm ${nativeTyping ? 'btn--primary' : 'btn--secondary'}`}
    onClick={() => setNativeTyping(!nativeTyping)}
  >
    Native typing
  </button>
</Tooltip>
```

### 13b. Run tests

```bash
./test.sh --quick
```

### 13c. Commit

```bash
git add assets/dashboard/src/routes/SessionDetailPage.tsx
git commit -m "feat(native-typing): per-session toggle in terminal toolbar"
```

---

## Step 14: End-to-end verification

### 14a. Manual verification checklist

1. **Toggle visibility**: Open a session. Verify "Native typing" button appears in the toolbar next to "Select lines".

2. **Toggle persistence**: Enable native typing, reload page. Verify it's still enabled. Disable, reload. Verify it's off.

3. **Basic typing**: Enable native typing. Type "hello world". Verify characters appear instantly (no round-trip delay). Press Enter. Verify the agent receives and processes the input.

4. **Backspace**: Type "helllo", press Backspace, verify last character is erased locally. Press Enter, verify agent receives "hello".

5. **Line wrapping**: Type a string longer than the terminal width. Verify wrapping looks correct. Press Enter, verify agent receives the full string.

6. **Backspace at wrap boundary**: Type past the terminal width, then Backspace back across the wrap boundary. Verify the cursor moves up correctly.

7. **Arrow key flush**: Type "hello", press Left arrow. Verify the buffer is flushed and the cursor moves per the agent's behavior (with latency).

8. **Ctrl+C**: Type "hello", press Ctrl+C. Verify the buffer is flushed and interrupt is sent.

9. **Unicode**: Type accented characters (cafe with acute e), emoji, CJK. Press Enter. Verify the agent receives them correctly.

10. **Paste**: Paste a multi-line string. Verify each line is sent correctly (Enter characters trigger flushes).

11. **Resize during typing**: Type "hello", resize the browser window. Verify the buffer is flushed and no display corruption.

12. **Scroll during typing**: Type "hello", scroll up with mouse wheel. Verify the buffer is flushed.

13. **Server output during typing**: Type slowly while an agent is producing output. Verify the buffer flushes and no corruption.

14. **Disabled state**: Disable native typing. Verify all keystrokes go through the normal path with no behavior change.

15. **Latency comparison**: With a remote server (~30ms+ ping), compare typing feel with native typing on vs off. Verify the latency improvement is perceptible.

### 14b. Run full test suite

```bash
./test.sh
```
