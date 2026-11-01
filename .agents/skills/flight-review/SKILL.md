---
name: flight-review
description: Review flight evidence with the user, identify intervention bottlenecks, and produce an evidence-backed product decision agenda.
disable-model-invocation: true
---

# Flight review

Sit down with the user over the evidence of how flights actually ran, and leave with a short list of product decisions. The evidence comes first; conclusions are the user's. Everything stays on this machine: never commit, push, or upload the report or the decisions file.

## 1. Pick the range and the cohort

- If the user gave a date range, use it. If not, ask for one only when they seem to want a specific period; otherwise default to the last 30 days (`date -v-30d +%F` on macOS, `date -d '30 days ago' +%F` on Linux, through `date +%F`).
- State which cohort rule the report uses, and never mix the two without labeling both:
  - `disposed`: flights the ledger recorded as removed in the range.
  - `activity`: flights removed in the range, plus active or disposed flights with recorded activity in it.

## 2. Find the previous decisions

Take the newest `./review/flight-review-decisions-*.md` (sort by name; the date is in it). If there is none, skip this. Its unresolved items (`Needs data`, `Deferred`) and its `Confirmed external completions` and `Confirmed mechanisms` feed the report.

## 3. Render the report

Call the daemon, sending the previous decisions as the body when you have them. `since` and `until` are inclusive `YYYY-MM-DD` dates.

```bash
umask 077
mkdir -p ./review
OUT="./review/flight-review-$(date +%F).html"
curl -sS -X POST \
  "http://localhost:7337/api/flights/review?since=$SINCE&until=$UNTIL&cohort=$COHORT" \
  --data-binary @"$PREVIOUS_DECISIONS_FILE" \
  -o "$OUT.tmp" -w '%{http_code}'
```

Omit `--data-binary` when there are no previous decisions.

Before keeping the response:

- Refuse and stop if `$OUT` would resolve outside `./review/` (no `..`, no symlink out of the directory), or if `git ls-files --error-unmatch -- "$OUT"` succeeds (the path is tracked). Say which check failed; do not write the file.
- Keep it only on HTTP 200 with a `Content-Type` of `text/html`. Otherwise show the JSON `error` and stop: `400` is a bad request, `503` means flights or the review service are unavailable.
- Move the temp file into place and `chmod 600 "$OUT"`.

Opening the report may take a while: it refreshes the evidence archives and asks Jev to label new operator messages. A missing TypeSafe key is not an error; those messages stay unlabeled and the report says so.

## 4. Meet in this order

Read the saved report and present it section by section. Do not draw a conclusion before the user has seen the data quality.

1. **Data quality and coverage.** Baseline-imported flights, missing transcripts, malformed ledger lines, git-state failures, recovered events, failed classifications, unlinked conversations. Say which aggregates are too thin to trust. Unlinked conversation candidates are never attached on your own; ask the user to confirm each.
2. **Orphan inventory.** Only after coverage is understood, go through each orphaned flight and ask for its disposition. For an outcome shown as `pending confirmation`, ask whether the branch was the result the user actually used, and record the answer as a confirmed external completion.
3. **Exceptions and evidence gaps.** One-flight findings and missing evidence. They are not recurring patterns.
4. **Interventions by purpose.** Keep product guidance (`product_guidance`) apart from routing, workflow, environment, approval, status, takeover, `mixed`, and `unclear` labels. Show `unclear`, `mixed`, `unlabeled`, and low-confidence counts as their own groups; do not fold them into another.
5. **Candidates.** For every candidate say:
   - the numerator, the covered denominator, and the cohort denominator, exactly as the report states them;
   - the evidence IDs it rests on;
   - the smallest experiment, the intervention it removes, the new failure it could introduce, and the counter-metric that must not regress. The report's wording is a starting point; refine it with the user.
     A recurring recommendation needs at least two affected flights, or one flight plus a mechanism the user confirms in the meeting. Otherwise it stays an exception.
6. **Choose one or two experiments** and define each one's success metric.

## 5. Record the decisions

Write `./review/flight-review-decisions-$(date +%F).md` (mode 600, same path checks as above) in exactly this shape; the next run reads it:

```markdown
# Flight review decisions — YYYY-MM-DD

Range: SINCE to UNTIL · cohort: disposed|activity

## Accepted

- c-<id>: what was accepted, the experiment, its success metric, its counter-metric

## Rejected

- c-<id>: why

## Needs data

- c-<id>: what evidence is missing

## Deferred

- c-<id>: why it waits

## Confirmed external completions

- fl-<id>: finished outside the flight workflow; what was used

## Confirmed mechanisms

- c-<id>: the mechanism the user confirmed
```

Carry every unresolved item from the previous file forward under `Needs data` or `Deferred` unless the user settled it this time. Leave a section's list empty rather than inventing an entry.

## Rules

- Never edit the generated report; decisions live only in the decisions file.
- Never commit, push, or upload either file. `review/flight-review-*.html` and `review/flight-review-decisions-*.md` are git-ignored on purpose; if either shows up in `git status`, stop and tell the user.
- Quote operator text only from the report. Do not read transcripts unless the user asks about one specific item.
