---
name: scenario
description: Write a scenario test for a feature you just implemented. Examines the current diff to understand what changed, writes a plain English scenario, then generates the Playwright test.
---

# Write a Scenario Test

You just implemented a feature or fixed a bug. Now write a scenario test that validates the user-facing behavior.

## Process

1. **Examine what changed:**
   - Run `git diff --name-only` to see modified files
   - Read the changed files to understand what user-facing behavior was added or modified
   - Focus on: new routes, new API endpoints, new UI interactions, changed workflows

2. **Check existing scenarios:**
   - Read all files in `test/scenarios/*.md`
   - Check if an existing scenario already covers this behavior
   - If yes, update the existing scenario instead of creating a new one

3. **Write the scenario:**
   - Create a new file in `test/scenarios/<descriptive-name>.md`
   - Follow the format from existing scenarios (see `test/scenarios/spawn-single-session.md` as reference)
   - Sections: title, description paragraph, `## Preconditions`, `## Verifications`
   - Write from the user's perspective — what goal are they trying to accomplish?
   - Verifications should mix UI checks and API checks

4. **Generate the test:**
   - Invoke the `/generate-scenario-tests` skill to regenerate all Playwright tests
   - Or manually write the `.spec.ts` file following the template pattern

5. **Verify:**
   - Run `cd test/scenarios/generated && npx tsc --noEmit` to check types
   - Present the scenario file and generated test for review

## Rules

- One scenario per user goal (not per code change)
- Keep scenarios focused — a scenario for "spawn a session" should not also test "edit nickname"
- Use plain English, not code, in the scenario file
- The scenario file is the source of truth; the generated test is derived from it
