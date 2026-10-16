# Plan Usage provider links

**Date:** 2026-10-11
**Status:** Proposed design, not yet implemented

## Goal

Each provider name listed below opens that provider's usage page from the sidebar's Plan Usage panel.

## Current State

Provider names in the Plan Usage panel are plain text.

## Behavior

- Clicking a provider name opens its usage page in a new tab.
- At rest, the name looks as it does today: quiet, no link styling.
- On hover, the name shows link styling: accent color, underline, pointer cursor.
- The names are not keyboard tab stops.
- A provider not in the table below keeps its plain-text name, with no link.

| Provider | Usage page                                              |
| -------- | ------------------------------------------------------- |
| Claude   | `https://claude.ai/settings/usage`                      |
| Codex    | `https://chatgpt.com/settings/usage`                    |
| Kimi     | `https://www.kimi.ai/code/console`                      |
| GLM      | `https://z.ai/manage-apikey/coding-plan/personal/usage` |
| MiniMax  | `https://platform.minimax.io/console/usage`             |

## Out of Scope

- No backend, API contract, or generated-type changes.
- No provider authentication, URL configuration, or redirect handling.
- No changes to provider usage calculations or the panel's polling and collapse behavior.
