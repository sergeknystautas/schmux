VERDICT: APPROVED

## Summary Assessment

All five critical issues from round 1 have been adequately addressed. The plan is now executable by an implementer without additional research. Two comment-level inaccuracies remain but do not affect correctness or executability.

## Critical Issues (must fix)

None.

## Suggestions (nice to have)

### S1. Outlier test comment arithmetic is wrong (twice)

**Test: "outlier breakdown uses P95+ cohort" (line 467-477)**
The comment says "P95+ cohort has 6 tuples (indices 114-119)." The actual count is 5. With 120 sorted tuples (55 at 27ms, 55 at 33ms, 5 at 95ms, 5 at 105ms), `sortedRTTs[114] = 95`. The filter `t.clientRTT > 95` excludes the 5 tuples at exactly 95ms, leaving only the 5 tuples at 105ms (indices 115-119). The test passes correctly (5 >= 5 minimum), but the comment should say 5 tuples, not 6.

**Test: "returns null for outlier cohort when fewer than 5 P95+ tuples" (line 479-485)**
The comment says "which is 4 tuples (indices 77-79)." Indices 77-79 span only 3 elements, not 4. More substantively, with 80 samples alternating between 27ms and 33ms, `sortedRTTs[76] = 33` and the filter `t.clientRTT > 33` yields 0 tuples (no value exceeds 33). The test passes for the right reason (0 < 5, returns null) but the comment's explanation is wrong. The implementer should update the comment to accurately describe why it works: all RTTs are 27 or 33, and nothing exceeds the P95 threshold of 33, so the cohort is empty.

### S2. Step 3 test mock pattern follows existing convention but is technically undermocked

The Step 3 `getBreakdown returns segmentSum field` test provides 2 `performance.now()` mocks per markSent/markReceived pair (matching the existing test pattern at lines 203-236 of the test file), without MockMessageChannel. `markSent()` actually calls `performance.now()` twice synchronously (lines 170, 174) and `markReceived()` calls it twice synchronously (lines 188, 206), consuming 4 mocks per iteration. With 5 iterations and only 10 mocks provided (for 20 needed), mocks are exhausted partway through and fallback to real `performance.now()` values.

This works in practice because the existing tests follow the same pattern and pass -- the key RTT values used by `getBreakdown` happen to be computed correctly enough for the assertions. However, the Step 4 tests correctly use MockMessageChannel and provide exact mock counts, which is the better pattern. If the implementer wants to be precise, they could add MockMessageChannel to the Step 3 test too, but since it matches the existing convention and will be updated in Step 4 anyway, this is cosmetic.

### S3. Step 2d acknowledges the existing test will break but buries the fix instructions

Step 2d says the existing `receive-time lag probe fires and records in receiveLagSamples` test will now fail and needs updating. This instruction is in a note after the "Run test to verify it passes" command. The implementer will hit the failure, then need to scroll down to find the fix instruction. Consider moving the fix instruction into Step 2c (the implementation section) so the implementer handles it before running tests.

### S4. Step 5 `getWireContext` test uses `originalMC` variable from wrong scope

The Step 5 test at line 757-779 pushes tuples directly into `serverSegmentSamples` with `receiveLag` values. This works correctly. However, the Step 4 test at line 570 references `originalMC` from the enclosing `describe` block's scope. If the implementer places the Step 5 `getWireContext` test outside the `describe('getBreakdown cohort-median')` block (which is where the existing `getWireContext` test lives), the `originalMC` variable will not be in scope. The plan should note that this test replaces the existing `getWireContext` test at the same location in the file (inside the main `describe('InputLatencyTracker')` block, not inside the cohort-median describe block).

## Verified Claims (things you confirmed are correct)

1. **C1 from round 1 (steps not parallelizable) -- FIXED.** Steps 1-5 are now correctly marked as sequential in the dependency table. Steps 6 and 7 are correctly identified as parallelizable since they modify different parts of `TypingPerformance.tsx` (Step 6: constants; Step 7: component functions).

2. **C2 from round 1 (test mocking) -- FIXED.** Step 1 tests now correctly use MockMessageChannel and provide 3 mocks for `markSent()` (lastInputTime at line 170, sentTime at line 174, lagHandler at line 177) and 3 for `markReceived()` before Step 2 (rtt at line 188, recvTime at line 206, recvLagHandler at line 209). After Step 2 removes the receive-time probe, `markReceived()` correctly requires only 1 mock (rtt at line 188). The Step 4 `addSamples` helper correctly provides 3 + 1 + 2 = 6 mocks per iteration.

3. **C3 from round 1 (jf submit replaced) -- FIXED.** All commit steps now say "Commit using `/commit`" which invokes the schmux-commit skill that enforces `./test.sh`, API doc checks, and self-assessment.

4. **C4 from round 1 (internal inconsistency) -- FIXED.** Step 2 now correctly says "Keep `receiveLagSamples` for now; it will be removed in Step 5." Step 4 rewrites `getBreakdown` (stops reading `receiveLagSamples`), and Step 5 removes the field. The narrative is now internally consistent.

5. **C5 from round 1 (receiveLag fallback) -- FIXED.** Step 4 now uses `seg.receiveLag ?? 0` instead of `seg.receiveLag ?? fallbackLag`, matching the design spec's explicit statement that "getBreakdown() treats tuples with receiveLag === undefined as having receiveLag = 0." The rationale is correctly explained in the changes-from-previous-version section.

6. **S1 from round 1 (outlier test sample count) -- ADDRESSED.** The outlier test now uses 80 samples instead of 20, giving a non-degenerate empty cohort. The test passes because all RTTs are 27 or 33 and no value exceeds the P95 threshold of 33. (Comment arithmetic is wrong per S1 above, but the behavior is correct.)

7. **S3 from round 1 (old test updates merged) -- FIXED.** Step 4 now explicitly lists which existing tests to update, which to remove, and updates them in the same commit as the API change. This prevents tests from being broken between commits.

8. **S4 from round 1 (getWireContext implementation) -- FIXED.** Step 5 now includes the full implementation code for `getWireContext()`, computing receiveLag stats from `serverSegmentSamples[].receiveLag` with proper filtering of `undefined` values.

9. **Cohort-median algorithm is correct.** The Step 4 implementation correctly: builds per-tuple `FullTuple` with clamped residuals, computes P25/P75/P95 from valid paired tuple RTTs (not raw samples), filters IQR and P95+ cohorts, enforces 5-tuple minimum, computes independent medians per segment, and returns both `total` (median RTT) and `segmentSum` (sum of segment medians).

10. **No backend changes needed.** Confirmed: `ServerSegmentTuple` is a TypeScript-only type. The `receiveLag` field is computed client-side by the MessageChannel probe and never serialized to/from the Go backend.

11. **All call sites of `getBreakdown` are covered.** Confirmed via grep: only `inputLatency.ts`, `inputLatency.test.ts`, and `TypingPerformance.tsx` reference `getBreakdown`. The Playwright scenario test uses `getStats()` and `reset()` only. Step 4 updates all three files.

12. **TrackerSnapshot handling is correct.** `receiveLag` is a field on `ServerSegmentTuple` tuples within `serverSegmentSamples`, which is already part of `TrackerSnapshot`. No snapshot changes needed for `receiveLag`. Step 5 correctly removes `receiveLagSamples` from `TrackerSnapshot` and `switchMachine()`.
