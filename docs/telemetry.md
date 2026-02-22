# Telemetry Privacy Notice

schmux collects anonymous usage data to help improve the product. This page explains what we collect and how to opt out.

## What We Collect

When telemetry is enabled, schmux sends the following anonymous events:

| Event               | When Triggered            | Properties                                 |
| ------------------- | ------------------------- | ------------------------------------------ |
| `daemon_started`    | Daemon starts             | `version`                                  |
| `workspace_created` | Creating a new workspace  | `workspace_id`, `repo_host`, `branch`      |
| `session_created`   | Spawning a new session    | `session_id`, `workspace_id`, `target`     |
| `push_to_main`      | Pushing to default branch | `workspace_id`, `branch`, `default_branch` |

## What We Don't Collect

We intentionally **do not** collect:

- Repository names or URLs
- File paths
- Branch names (beyond the anonymized `branch` field which may contain feature names)
- User identifiers (names, emails, etc.)
- Code content
- Prompt content
- Any other personally identifying information

Each installation is assigned a random UUID (`installation_id`) for anonymous tracking. This ID is not linked to any personal information.

## How to Opt Out

Telemetry is enabled by default. To disable it, add this to your `~/.schmux/config.json`:

```json
{
  "telemetry_enabled": false
}
```

## Why We Collect This Data

The telemetry helps us understand:

- How many active installations exist
- Which features are being used
- Whether pushes to main are succeeding

This data guides product decisions and helps prioritize improvements.

## Data Retention

Events are sent to PostHog and retained according to their standard retention policies. The data is not shared with third parties.
