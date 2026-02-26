---
allowed-tools: Bash(./test.sh*)
description: Run the test suite (./test.sh)
---

Run `./test.sh` now and wait for it to complete.

Pass through any arguments the user provided. For example:

- `/test` → `./test.sh`
- `/test quick` → `./test.sh --quick`
- `/test e2e` → `./test.sh --e2e`
- `/test scenarios` → `./test.sh --scenarios`

Report the results clearly: which test suites ran, how many passed/failed, and any failure details.
