# Install verification test

**Status:** design
**Date:** 2026-05-08

## Problem

The last two releases (v1.2.0, v1.2.1) shipped with zero assets because the release workflow's test step failed before binaries were built or uploaded. The installer curls the latest release and tries to download assets that don't exist, failing with a confusing "Failed to download checksums" error. There is no automated test that catches this.

## Goal

A standalone Docker-based test that simulates a fresh user installing schmux from the real GitHub release. Runs after a release is published and verifies the install actually works. Separate from `test.sh` — this tests the distribution pipeline, not local code.

## Files

| File                                   | Status             | Purpose                                                    |
| -------------------------------------- | ------------------ | ---------------------------------------------------------- |
| `install.sh`                           | stays at root      | Preserves public curl URL                                  |
| `release/release-strategy.md`          | moved from `docs/` | Release process docs                                       |
| `release/Dockerfile.install-test`      | new                | Minimal Debian container with `curl`, `tar`, `tmux`        |
| `release/test-install.sh`              | new                | Builds image, runs container, reports pass/fail            |
| `.github/workflows/verify-release.yml` | new                | Triggers after release.yml, runs `release/test-install.sh` |
| `docs/README.md`                       | updated            | Fix link to moved `release-strategy.md`                    |

## Container design

Based on `debian:bookworm-slim`. Installs only what a real user would have: `curl`, `tar`, `tmux`, `bash`, `git`. Creates a non-root user. No Go, no Node.

The entrypoint:

1. Curls `install.sh` from `raw.githubusercontent.com/sergeknystautas/schmux/main/install.sh` and pipes to bash
2. Adds `~/.local/bin` to PATH
3. Asserts `schmux --version` exits 0 and prints a version string
4. Asserts `~/.schmux/dashboard/index.html` exists
5. Runs `schmux start`, asserts exit 0
6. Runs `schmux status`, asserts exit 0
7. Runs `schmux stop`

Each step prints what it's doing. On failure, prints which step failed and exits non-zero.

## `release/test-install.sh`

Builds the Docker image from `release/Dockerfile.install-test`, runs the container, and exits with the container's exit code. No arguments.

## `.github/workflows/verify-release.yml`

Triggers on `workflow_run` of the Release workflow, filtered to completed successfully. Checks out the repo and runs `release/test-install.sh`.
