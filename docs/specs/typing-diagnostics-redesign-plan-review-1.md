VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is well-structured and covers the design requirements thoroughly, but has three critical issues: Steps 1-3 are claimed as parallelizable yet all modify `inputLatency.ts` with overlapping concerns (Step 3's test calls `getBreakdown('p50')` which will break after Step 4 changes the signature); Step 1's test mocking is wrong for the current code (under-mocking `performance.now()` calls); and the plan uses `jf submit` for commits when the project's CLAUDE.md requires the `/commit` skill.

## Critical Issues (must fix)

### C1. Steps 1-3 are NOT safely parallelizable -- Step 3's test breaks at Step 4

The dependency table says Steps 1, 2, and 3 can be parallelized as "independent changes to different parts of inputLatency.ts." This is incorrect:

- All three steps modify `inputLatency.ts`. Even if the exact line ranges don't overlap, three parallel agents editing the same file will produce merge conflicts.
- More importantly, **Step 3's test calls `getBreakdown('p50')`** (line 224 of the plan). Step 4 changes the method signature to `getBreakdown('typical' | 'outlier')`. This means Step 3's test will pass when written, then **break in Step 4** and need rewriting. This is wasted work. Step 3 should use the new API from the start, which means it depends on Step 4's signature decision. The plan should either: (a) make Steps 1-3 sequential, or (b) have Step 3's test already use `'typical'` and defer the test-passing verification until Step 4 completes.

### C2. Step 1 test mocking is wrong -- insufficient `performance.now()` mocks

The Step 1 staleness test mocks `performance.now()` like this:

```typescript
mockNow.mockReturnValueOnce(100); // markSent
mockNow.mockReturnValueOnce(2600); // markReceived rtt
mockNow.mockReturnValueOnce(2600); // "receive lag probe timestamp"
```

But looking at the actual code, `markSent()` calls `performance.now()` **twice** (line 170 for `lastInputTime` and line 174 for `sentTime` in the lag probe), and `markReceived()` calls it **twice** (line 188 for `rtt` and line 206 for `recvTime` in the receive lag probe). Since `markSent()` also fires a MessageChannel probe that calls `performance.now()` in its handler, and existing tests with MockMessageChannel show this fires synchronously in the test environment, the mock needs at minimum 3 return values for `markSent()` alone (lastInputTime, sentTime, lagHandler), not 1. Without the MockMessageChannel setup, the real MessageChannel's handler will fire asynchronously and consume an unexpected mock value, potentially causing the test to pass for the wrong reason or fail nondeterministically.

The test for "keeps samples within staleness threshold" has the same issue.

Compare with the existing test at line 393-404 of the test file (`event loop lag tracker records samples via markSent`), which correctly provides 3 mock values for `markSent` and uses a MockMessageChannel.

### C3. Commit commands use `jf submit` instead of `/commit`

Every step's commit command uses `jf submit -m "..."`. The project's CLAUDE.md states:

> **ALWAYS use `/commit` to create commits. NEVER run `git commit` directly.**

`jf submit` is neither `git commit` nor `/commit`. The CLAUDE.md's pre-commit enforcement (running `./test.sh`, checking API docs, requiring self-assessment) only works through the `/commit` skill. All commit steps should use `/commit`.

### C4. Step 4's residual formula does not match the design spec

The design spec says (section "Residual computation"):

```
residual = max(0, clientRTT - handler - transport - tmuxAgent - wsWrite - jsQueue - xterm)
```

The plan's Step 4 implementation computes it as:

```typescript
const measured = serverTotal + xterm + jsQueue;
const network = Math.max(0, clientRTT - measured);
```

where `serverTotal = seg.dispatch + seg.sendKeys + seg.echo + seg.frameSend`. This is correct algebraically. However, the plan also removes `receiveLagSamples` support from `getBreakdown()` in Step 4, but **does not remove the `receiveLagSamples` field itself until Step 5**. Meanwhile, Step 2 says "Keep `receiveLagSamples` array for now (existing `getBreakdown` still reads it). It will be removed in Step 4 when `getBreakdown` is rewritten." But Step 4's implementation does NOT remove `receiveLagSamples` -- it just stops reading it. Step 5 removes it. The plan's own Step 2 incorrectly says Step 4 removes it. This internal inconsistency will confuse the implementer.

### C5. Design spec says `receiveLag === undefined` maps to 0, but plan uses `fallbackLag` from `lagSamples`

The design spec section 4 states:

> `getBreakdown()` treats tuples with `receiveLag === undefined` (probe has not fired yet or MessageChannel unavailable) as having `receiveLag = 0`.

But the plan's Step 4 implementation uses:

```typescript
const jsQueue = seg.receiveLag ?? fallbackLag;
```

where `fallbackLag` is the median of `lagSamples`. This contradicts the design's explicit rule. The design does mention a MessageChannel-unavailable fallback, but that's an environment-level fallback (all tuples lack receiveLag because MessageChannel doesn't exist), not a per-tuple fallback where some have it and some don't.

The plan should either follow the design exactly (`receiveLag ?? 0`) or explicitly note it's deviating from the design with justification.

## Suggestions (nice to have)

### S1. Step 4's outlier test ("returns null for outlier cohort when fewer than 5 P95+ tuples") may be unreliable

The test creates 20 samples and expects P95+ to have fewer than 5 tuples. With 20 samples, `Math.floor(20 * 0.95) = 19`, so P95+ is `tuples.filter(t => t.clientRTT > p95)` where `p95 = sortedRTTs[19]`. For 20 items, index 19 is the maximum, so there are 0 tuples strictly greater than the maximum. This will always be 0 tuples (empty cohort, returns null) -- the test passes but for a degenerate reason. With `addSamples(20, 30, 3)`, the RTTs alternate between 27 and 33, so sorted[19]=33 and no tuples have RTT > 33. A more robust test would use a sample size where P95+ gives exactly 3-4 tuples (below 5 minimum) -- e.g., 100 samples gives 5 at P95+, so use 80 (which gives 4).

### S2. Step 4 test helper `addSamples` produces interleaved RTTs but mockNow calls may be undercounted

The `addSamples` helper calls `markSent()` and `markReceived()` in a loop. As noted in C2, `markSent()` calls `performance.now()` at least twice (plus once more for the MessageChannel handler in test environments), and `markReceived()` calls it at least twice. The helper provides only `mockReturnValueOnce(100)` and `mockReturnValueOnce(100 + rtt)` per iteration -- that is 2 mocks per loop, but the code calls `performance.now()` at least 4 times. This means the mocks will be exhausted partway through the loop, and subsequent calls will return `undefined`, causing NaN propagation.

This is the same root cause as C2 but affects all of Step 4's tests via the shared helper.

### S3. Step 8 is vague -- "Update all ... calls" is not a well-defined task

Step 8 says to update all `getBreakdown('p50')` to `'typical'`, remove tests that reference `receiveLagSamples`, etc. But at this point in the plan, the old tests should already be failing (Step 4 changed the API). Step 8 should be done as part of Step 4, not deferred. Having broken tests for multiple steps (5, 6, 7) is poor hygiene and means `./test.sh --quick` will fail in every intermediate step.

### S4. `getWireContext()` updates are not fully specified in Step 5

Step 5 says to "Update `getWireContext()` to compute receiveLag stats from `serverSegmentSamples[].receiveLag` instead of the removed array." But the implementation is not shown. The current `getWireContext()` returns `receiveLagP50` and `receiveLagP99` (lines 84-85 of inputLatency.ts). After removing `receiveLagSamples`, these need to be computed from `serverSegmentSamples.map(s => s.receiveLag).filter(v => v !== undefined)`. This is a non-trivial change that should have implementation code in the plan.

### S5. No task for updating `TrackerSnapshot` with `receiveLag`

Step 2 adds `receiveLag` to `ServerSegmentTuple`, and `serverSegmentSamples` already carries this type. But the design spec notes: "if any new fields are added to the tracker state, the `TrackerSnapshot` type and the `switchMachine()` save/restore logic must be updated to include them." Since `receiveLag` is added to individual tuples within `serverSegmentSamples` (which is already in `TrackerSnapshot`), the snapshot will automatically carry the new field. This is fine, but worth verifying explicitly. No action needed -- just confirming the plan's implicit assumption is correct.

### S6. Task sizing -- most tasks are 2-5 minutes but Step 4 is significantly larger

Step 4 requires: writing 8 tests, a complete rewrite of `getBreakdown()` (the most complex method in the file), updating call sites in `TypingPerformance.tsx`, and verifying everything passes. This is realistically 15-20 minutes of focused work, not 2-5 minutes. Consider splitting it into sub-steps: (4a) rewrite `getBreakdown` with cohort-median logic, (4b) update TypingPerformance.tsx call sites, (4c) verify all tests pass.

## Verified Claims (things you confirmed are correct)

1. **File paths are accurate.** All three implementation files exist at the stated paths: `assets/dashboard/src/lib/inputLatency.ts` (444 lines), `assets/dashboard/src/lib/inputLatency.test.ts` (557 lines), `assets/dashboard/src/components/TypingPerformance.tsx` (359 lines). `docs/typing-performance.md` exists for Step 9.

2. **No backend changes needed.** Confirmed: `LatencyBreakdown` and `getBreakdown()` are purely frontend. `ServerSegmentTuple` is a TypeScript type -- adding `receiveLag` to it requires no Go changes since the field is computed client-side by the MessageChannel probe, not sent from the server.

3. **Current `getBreakdown` uses single-tuple picking.** Confirmed at lines 408-424 of `inputLatency.ts`: it finds the tuple with `clientRTT` closest to the histogram's target RTT percentile.

4. **Current minimum paired count is 3.** Confirmed at lines 345-351 of `inputLatency.ts`.

5. **`receiveLagSamples` is a separate array not index-aligned with tuples.** Confirmed: `receiveLagSamples` is pushed independently in `markReceived()` (line 210), while `serverSegmentSamples` is pushed in `recordServerSegments()` (line 221). These are called from different code paths.

6. **`TypingPerformance.tsx` currently uses `'p50'` and `'p99'` string literals.** Confirmed at lines 343-344: `inputLatency.getBreakdown('p50')` and `inputLatency.getBreakdown('p99')`.

7. **No other consumers of `getBreakdown` outside the two known files.** Searched the entire `assets/dashboard/src` tree -- only `inputLatency.ts`, `inputLatency.test.ts`, and `TypingPerformance.tsx` reference it. The Playwright scenario test (`typing-latency.spec.ts`) uses `__inputLatency` but only calls `getStats()` and `reset()`, not `getBreakdown()`.

8. **The cohort-median algorithm matches the design spec.** The plan's Step 4 correctly implements IQR (P25-P75) for typical and P95+ for outlier, computes per-tuple residuals clamped to zero, takes cohort medians of all segments independently, and returns both `total` (cohort median RTT) and `segmentSum` (sum of segment medians).

9. **Minimum cohort size of 5 is correctly enforced.** The plan's Step 4 checks `if (cohort.length < 5) return null;` matching the design's requirement.

10. **The plan does NOT handle old-style `'p50'`/`'p99'` arguments after the API change.** The signature changes from `'p50' | 'p99'` to `'typical' | 'outlier'` with no backward compatibility. This is acceptable because (a) all call sites are updated in Step 4, (b) TypeScript will catch any missed call sites at compile time, and (c) there are no external/dynamic callers (the Playwright tests don't call `getBreakdown`).
