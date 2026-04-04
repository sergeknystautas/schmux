VERDICT: NEEDS_REVISION

## Summary Assessment

The plan covers the design requirements well and the overall architecture is sound, but there are several critical correctness issues in the code snippets (backspace wrap logic, UTF-8 range exclusion, handleOutput flush during bootstrap, paste handling placement) and a structural problem where Step 2's tests cannot be written as shown because TerminalStream has no trivially-constructible form for unit testing.

## Critical Issues (must fix)

### 1. UTF-8 classifier excludes valid 2-byte leading bytes 0xC0-0xC1

Step 1c's comment says `0xC0-0xDF: 2-byte` but the range check `b >= 0xC0 && b <= 0xF7` is correct for entering the branch. However, 0xC0 and 0xC1 are technically overlong encodings (they encode code points that fit in a single byte). Go's `string` type can contain arbitrary bytes, and tmux `send-keys -l` passes raw bytes, so this might not be a real problem in practice. But more importantly: **the `default` case at the bottom of the switch (line 136) will silently drop any byte that doesn't match `ch >= 1 && ch <= 26`.** A multi-byte UTF-8 character's leading byte (e.g., 0xC3 for `e with acute`) falls through from the printable loop, enters the switch's `default`, fails the `ch >= 1 && ch <= 26` check, produces no `keyName`, and the `if keyName != ""` guard means `advance` is 1 but no run is appended. The byte is silently skipped. This is the bug the plan is fixing, but the plan's new UTF-8 code is inserted _inside_ the existing printable-character loop (the `j` loop at line 40). It needs to be placed correctly: the plan says "Change the printable character loop (line 40)" which means replacing the inner `for j < len(keys)` loop body. This is correct placement. Verified.

However, there is a subtle issue: the plan adds `continue` after `j += size` inside the UTF-8 branch, which continues the inner `for j < len(keys)` loop. But the `break` statements also correctly break out of the inner loop. **The logic is correct** -- the `continue` skips back to the `for j < len(keys)` condition check, and `break` exits the inner loop to check `if j > i`.

**No change needed** - on re-analysis this is correct.

### 2. Backspace wrap boundary escape sequence is wrong (Step 6)

The wrap boundary code writes:

```
\x1b[A           // Move up one row
\x1b[${targetCol}G  // Move to column
' '.repeat(width)   // Overwrite with spaces
\x1b[A           // Move up again (space advanced cursor)
\x1b[${targetCol}G  // Reposition
```

This has **two problems**:

**Problem A**: `targetCol` is computed as `cols - width + 1` (1-indexed). For a single-width char on an 80-column terminal, that's column 80. But writing a space at column 80 causes xterm.js to wrap the cursor to row+1, column 0. Then writing `\x1b[A` moves up one row and `\x1b[${targetCol}G` repositions. The sequence works if writing at the last column wraps. But for a **2-wide character**, `targetCol = 80 - 2 + 1 = 79`, and writing 2 spaces starting at column 79 fills columns 79-80, wrapping the cursor. The `\x1b[A` + reposition then works. This seems correct on closer inspection.

**Problem B**: The first `\x1b[A` moves up. Then `\x1b[${targetCol}G` positions to the right column. Then `' '.repeat(width)` writes spaces (advancing cursor, potentially wrapping). Then `\x1b[A` moves up again. **But we only moved down one row total** (the original wrap from typing). Moving up twice from one row down means we end up one row above where we should be. The second `\x1b[A` assumes the space-write caused a wrap (advancing us back down one row), which only happens if the space was written at the last column(s). For a single-width char at column 80, the space wraps, so `\x1b[A` compensates. For a 2-wide char at column 79, writing 2 spaces fills to column 80 and wraps, so `\x1b[A` also compensates. **This works only if the character being erased was at the last columns.** But what if a single-width character at column 80 does NOT cause a wrap because xterm.js deferred-wrap semantics mean the cursor sits at column 80 (past the last column) without actually wrapping until the next character? In that case the space write would NOT wrap, and the second `\x1b[A` would move up too far.

**This is a real risk.** xterm.js uses deferred/pending wrap (a.k.a. "wrap pending" state): after writing to the last column, the cursor stays at the last column with a pending-wrap flag. The next character triggers the wrap. So writing a space at column 80 does NOT wrap -- it overwrites column 80 and sets the pending-wrap flag. The `\x1b[A` then moves up from the current row, not from a wrapped row. The second `\x1b[A` moves up yet again, leaving the cursor **two rows above the original position** instead of one.

**Fix**: The wrap-boundary backspace should be:

```typescript
const cols = this.terminal.cols;
const targetCol = cols - width + 1;
this.terminal.write(
  `\x1b[A` + // Move up one row
    `\x1b[${targetCol}G` + // Move to column
    ' '.repeat(width) + // Overwrite with spaces
    `\x1b[${targetCol}G` // Reposition (no second \x1b[A needed)
);
```

After writing spaces at the end of the line, the cursor is at the same row (deferred wrap, or at most at col 0 of next row if wrap fires). Using CUG (`\x1b[...G`) to reposition on the same line is simpler and correct. But actually, if the write DID trigger a wrap (which can happen), then we need `\x1b[A`. The safest approach is to use CUP (`\x1b[row;colH`) to position absolutely, using the cursor position we know we want to be at.

### 3. handleOutput flush fires during bootstrap (Step 9c)

Step 9c adds this at the top of `handleOutput`:

```typescript
if (this.localBuffer.length > 0 && data instanceof ArrayBuffer) {
  this.flushLocalBuffer();
}
```

This runs **before** the bootstrap check. During initial connection, the first binary frame triggers a `terminal.reset()` + full write. If the user somehow had a non-empty localBuffer at that point (e.g., typed during reconnection while the previous buffer was still active), the flush would:

1. Write `\x1b[row;colH` to the terminal (cursor rewind)
2. Send the buffer to the server
3. Then the bootstrap resets the terminal

The cursor rewind writes to the terminal **before** `terminal.reset()` clears it, so it's harmless (immediately erased). But `sendRawInput` sends data to the server before the WebSocket has completed bootstrap negotiation. More importantly, the `nativeTypingEnabled` flag should be false at connection time (page load), so `localBuffer` should be empty. **This is a low-severity issue** -- the guard `this.localBuffer.length > 0` makes it nearly impossible to trigger during bootstrap since native typing is off by default and the buffer resets on reconnection. But the code would be cleaner if it checked `this.bootstrapComplete` as well:

```typescript
if (this.localBuffer.length > 0 && data instanceof ArrayBuffer && this.bootstrapComplete) {
  this.flushLocalBuffer();
}
```

### 4. Paste handling in Step 9d has incorrect placement in sendInput

Step 9d adds a paste handler for multi-character input, but it's described as a separate code block to add to `sendInput`. Looking at Step 3c's modified `sendInput`, the single-character path already handles `isBufferedInput(data)` for single chars. Step 9d's multi-character handler needs to be inserted **before** the single-character check, since `data.length > 1` is the distinguishing condition. The plan says:

```typescript
// In the native typing branch of sendInput, handle paste (multi-char input)
if (this.nativeTypingEnabled && this.followTail && data.length > 1) {
```

But Step 3c already has:

```typescript
if (this.nativeTypingEnabled && this.followTail) {
  if (isBufferedInput(data)) {
```

The `isBufferedInput` function checks `data.length === charLen` where `charLen` is 1 or 2 (for surrogates). So a paste string like `"hello"` (length 5) correctly falls through `isBufferedInput` (returns false) and hits the "Immediate key" path, which would flush + send the whole paste string as a raw input. **The paste handler from Step 9d must be placed before the `isBufferedInput` check**, and it's not clear from the plan where exactly it goes. The plan just says "In the native typing branch of sendInput" which is ambiguous. It needs to be the first check inside the `if (this.nativeTypingEnabled && this.followTail)` block, before the existing `isBufferedInput(data)` check.

Also, multi-code-unit emoji like `"hello"` -- wait, `isBufferedInput` with a single emoji like `"\uD83D\uDE80"` (rocket emoji, `data.length === 2`) returns true because `charLen` would be 2 and `data.length === charLen`. But a paste of multiple emoji `"\uD83D\uDE80\uD83D\uDE80"` (length 4) falls through -- this is correct, the paste handler would process it character by character.

**Fix needed**: The plan must explicitly specify the insertion order. The paste handler (`data.length > 1`) must come before the single-character `isBufferedInput` check.

### 5. Step 2a tests cannot work as written

Step 2a's test instantiates TerminalStream as:

```typescript
const stream = new TerminalStream('test-session', container, {});
```

But the existing test file (terminalStream.test.ts) shows the constructor requires a real-ish `container` with `getBoundingClientRect` mocked, and the constructor immediately calls `initTerminal()` which is async. The test accesses `stream.nativeTypingEnabled` synchronously without awaiting `stream.initialized`. While the field access itself is safe (it's a class field set at declaration time, not in the constructor body), the tests as written have no `beforeEach` setup or container creation. They need the same test setup boilerplate as the existing tests (creating a container element, etc.).

More importantly, the plan says this test file is `terminalStream.test.ts` "(add to existing test file, or create if needed)". The file already exists. The tests should follow the existing pattern with `beforeEach` setup and `await stream.initialized`.

**Fix**: Use the existing test file's container setup pattern. The tests should be added to the existing file with proper setup.

### 6. Step 2c's `flushLocalBuffer` stub calls `this.sendRawInput` which may not exist at class scope

The stub method calls `this.sendRawInput(this.localBuffer)` and `this.sendRawInput(trailingKey)`. This is fine since `sendRawInput` already exists on the class. No issue here. **Retracted.**

### 7. The `isBufferedInput` function does not handle Alt+letter combinations

The design doc says "Alt/Meta combinations (except those producing printable characters)" should be immediate. In xterm.js, Alt+a arrives via `onData` as `"\x1ba"` (ESC + 'a'). The `isBufferedInput` function sees `data.length === 2`, `codePointAt(0)` returns 0x1B (27), and since 27 < 32, it returns false. This is correct -- Alt+letter is treated as immediate. Good.

But what about Alt+letter combinations that arrive through `attachCustomKeyEventHandler`? Looking at the code, Alt+Enter and Alt+Backspace go through `sendInput`, which now has the native typing classification. `\x1b\r` has length 2, first code point 0x1B (27 < 32), so `isBufferedInput` returns false -- correct. `\x1b\x7f` has length 2, first code point 0x1B, returns false -- correct. These flush and send, which is right.

**No issue** -- retracted.

## Suggestions (nice to have)

### S1. Consider guarding `flushLocalBuffer` during bootstrap

Add `this.bootstrapComplete` check in Step 9c's handleOutput flush to be defensive:

```typescript
if (this.localBuffer.length > 0 && data instanceof ArrayBuffer && this.bootstrapComplete) {
```

### S2. Step 4 skips tests entirely

Step 4 says "No isolated test for this step -- it requires a live xterm.js terminal instance." But the existing test file already has mocked terminal instances that track `write` calls. A test could verify that `echoLocally` calls `terminal.write(char)` and updates `localBuffer` and `localEchoStart`. This would catch regressions without needing a live terminal.

### S3. Step 7 line reference is inaccurate

The plan says "In `handleSync()` (line ~1358)." The actual line is 1354 for the method signature. This is close enough but the plan should note that line numbers are approximate since earlier steps modify the file.

### S4. Step 8 line reference is inaccurate

The plan says "Modify `sendRawInput()` (line ~1054)." The actual line is 1054. This is correct currently, but by Step 8, Steps 2-7 have added substantial code to the file, so the actual line number will have shifted significantly.

### S5. The Ctrl+V check in sendInput should be verified for interaction

The plan's modified `sendInput` (Step 3c) places the native typing classification after the Ctrl+V check, which is correct -- `\x16` is handled first and returns early. But if native typing is enabled and `isBufferedInput('\x16')` were called: code point 0x16 is 22, which is < 32, so it returns false. It would hit the immediate path and flush. But it never gets there because the Ctrl+V early return comes first. This is fine, but worth noting in the plan for clarity.

### S6. Step 10 references `useLocalStorage.ts` but the actual file is `useLocalStorage.ts` under `hooks/`

The plan says to add `NATIVE_TYPING_PREFIX` to `useLocalStorage.ts`, but looking at the existing exports, the file only has simple string constants, not prefix patterns. The plan should clarify that the prefix constant is optional (it's not actually used anywhere in the implementation -- the key is constructed inline as `` `nativeTyping:${sessionId}` ``).

### S7. The plan uses `git commit` directly in commit commands

The CLAUDE.md says "ALWAYS use `/commit` to create commits. NEVER run `git commit` directly." Every step's commit command uses bare `git commit`. This violates the project conventions, though it's minor since this is a plan, not execution.

### S8. Task sizing for Steps 6 and 9

Step 6 (backspace with line-wrap and wide character support) is complex enough that the implementation plus debugging the escape sequences could take more than 5 minutes, especially with the wrap boundary edge case. Step 9 bundles four distinct edge cases (resize, scroll, server output, paste) into one step, each touching different parts of the file. Consider splitting Step 9 into separate steps.

## Verified Claims (things you confirmed are correct)

1. **File paths are accurate**: `internal/remote/controlmode/keyclassify.go` exists and contains the code described. `assets/dashboard/src/lib/terminalStream.ts` exists with the class structure described. `assets/dashboard/src/routes/SessionDetailPage.tsx` exists (plan correctly uses `routes/`, not `components/` as the design doc's table says). `assets/dashboard/src/hooks/useLocalStorage.ts` exists.

2. **keyclassify_test.go does not exist yet**: Confirmed no test file exists, matching plan's "(new file)" annotation.

3. **terminalStream.test.ts already exists**: Confirmed. Plan correctly notes "(add to existing test file, or create if needed)".

4. **The printable byte range bug is real**: Line 40 of keyclassify.go has `keys[j] >= 32 && keys[j] < 127`, which excludes all UTF-8 multi-byte sequences. Bytes >= 0x80 fall through to the switch/default, where they're silently dropped if not in the 1-26 control range.

5. **`sendInput` exists at line 1022 and `sendRawInput` at line 1054**: Confirmed.

6. **`handleOutput` is at line 1163**: Confirmed.

7. **`handleSync` is at line 1354**: Confirmed.

8. **Wheel handler at line 575**: The code `if (e.deltaY < 0 && this.followTail)` matches the plan's description.

9. **`fitTerminal` at line 714**: Confirmed.

10. **Unicode11Addon is loaded at line 331**: Confirmed, with `this.terminal.unicode.activeVersion = '11'` at line 332.

11. **`inputLatency.markSent()` is called in `sendRawInput` at line 1056**: Confirmed.

12. **`sendSyncResult` exists**: Confirmed at line 1417.

13. **Tooltip component is imported and used in SessionDetailPage**: Confirmed.

14. **useLocalStorage hook is imported in SessionDetailPage**: Confirmed at line 22.

15. **The TerminalStream constructor takes (sessionId, containerElement, options)**: Confirmed at line 237.

16. **The "Select lines" button is inside a `selectionMode` conditional**: Confirmed. When `selectionMode` is false, the button appears at line 824-831. The native typing toggle should be placed after this conditional block (after line 849's closing `</>`) but before the "Download log" button at line 851.

17. **Alt+Enter and Alt+Backspace go through `sendInput`**: Confirmed at lines 394 and 399 -- they call `this.sendInput('\x1b\r')` and `this.sendInput('\x1b\x7f')` respectively, so they'll correctly hit the native typing classification path.

18. **`isBufferedInput` logic for single characters is correct**: Printable chars (code point >= 32, single char length) return true. Backspace (`\x7f` = code point 127, but checked explicitly) returns true. Control chars (code point < 32) return false. Multi-byte escape sequences (length > charLen) return false.
