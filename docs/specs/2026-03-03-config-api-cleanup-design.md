# Config API Cleanup

## Problem

The `/api/config` model response is bloated. Runner details (available, configured, capabilities, required_secrets) are nested under every model, even though most are tool-level properties. Unused fields (category, usage_url, preferred_tool) add noise.

## Design

### Model (slim)

```json
{
  "id": "kimi-thinking",
  "display_name": "Kimi Thinking",
  "provider": "moonshot",
  "configured": true,
  "runners": ["claude", "opencode"],
  "required_secrets": ["ANTHROPIC_AUTH_TOKEN"]
}
```

- `runners` becomes `[]string` (just tool names)
- `required_secrets` moves from per-runner to model level
- `configured` stays (derived: at least one runner available + secrets met)
- Remove: `category`, `usage_url`, `preferred_tool`

### Top-level runners (tool-level properties)

```json
"runners": {
  "claude": { "available": true, "capabilities": ["interactive", "oneshot", "streaming"] },
  "opencode": { "available": true, "capabilities": ["interactive", "oneshot"] }
}
```

Added to `ConfigResponse`. Capabilities and availability are tool properties, defined once.

### Frontend impact

- **ModelCatalog**: cross-references top-level runners for availability; reads `model.required_secrets` directly
- **useConfigForm**: `oneshotModels` derivation looks up capabilities from top-level runners instead of per-model runners
- **Test fixtures**: remove `category` field from model objects

## Files to change

### Go

- `internal/api/contracts/config.go` — restructure Model, RunnerInfo, add Runners to ConfigResponse
- `internal/models/manager.go` — update GetCatalog() to build new shapes, return top-level runners
- `pkg/cli/daemon_client.go` — update duplicate Model struct
- Regenerate: `go run ./cmd/gen-types`

### Frontend

- `assets/dashboard/src/routes/config/ModelCatalog.tsx` — use top-level runners, model-level required_secrets
- `assets/dashboard/src/routes/config/useConfigForm.ts` — oneshotModels looks up top-level runners
- `assets/dashboard/src/routes/ConfigPage.tsx` — pass top-level runners to ModelCatalog
- Test fixtures in ~6 files — remove category
