# JSON Schema Structured Generation

**Status:** Complete
**Branch:** `feature/json-schema-alternative-2ht`
**Created:** 2026-02-13

## Problem

The oneshot system currently defines JSON schemas as long inline strings in `internal/oneshot/oneshot.go`:

```go
var schemaRegistry = map[string]string{
    SchemaConflictResolve: `{"type":"object","properties":{"all_resolved":{"type":"boolean"},...}`,
    SchemaNudgeNik:        `{"type":"object","properties":{"state":{"type":"string"},...}`,
    SchemaBranchSuggest:   `{"type":"object","properties":{"branch":{"type":"string"},...}`,
}
```

The corresponding Go structs are defined separately in their respective packages:

- `nudgenik.Result`
- `conflictresolve.OneshotResult` + `FileAction`
- `branchsuggest.Result`

This creates maintenance burden:

1. Schema strings are error-prone and hard to read
2. Schema and struct definitions can drift out of sync
3. No compile-time validation that schema matches struct

## Constraints

### OpenAI Structured Output Requirements

OpenAI's structured outputs with `strict: true` require:

- `additionalProperties: false` on all objects
- All fields must be in the `required` array (optional fields cause issues)
- Proper handling of nested objects and maps

### Why invopop/jsonschema Doesn't Work

The `invopop/jsonschema` library treats `json:",omitempty"` as "optional" (excludes from required array). OpenAI strict mode has issues with optional fields—the model may skip them or produce 400 errors.

## Solution: swaggest/jsonschema-go

Use `github.com/swaggest/jsonschema-go` which provides:

- Explicit `required:"true"` struct tags (opt-in, not opt-out)
- `additionalProperties:"false"` via unnamed struct field
- Rich constraint support (enum, minimum, pattern, etc.)
- Runtime schema generation from Go structs

### Alternative Libraries Evaluated

| Library                             | Verdict      | Notes                                             |
| ----------------------------------- | ------------ | ------------------------------------------------- |
| `swaggest/jsonschema-go`            | **Selected** | Explicit required tags, rich constraints          |
| `google/jsonschema-go`              | Good option  | New (Jan 2026), uses omitzero/omitempty semantics |
| `invopop/jsonschema`                | Rejected     | omitempty = optional, problematic for OpenAI      |
| `sashabaranov/go-openai/jsonschema` | Limited      | Docs say "fairly limited for complex schemas"     |

## Implementation Plan

### Phase 1: Add Dependency and Create Schema Package

- [x] Add `github.com/swaggest/jsonschema-go` to go.mod
- [x] Create `internal/schema/` package for centralized schema generation
- [x] Implement `GenerateJSON()` helper function with:
  - `InlineRefs` to avoid `$ref`/`definitions`
  - `InterceptProp` for field skipping
  - Post-processing to add empty `properties:{}` for OpenAI compatibility

### Phase 2: Migrate Struct Definitions

- [x] Updated structs with `required:"true"` and `nullable:"false"` tags
- [x] Added `additionalProperties:"false"` via unnamed struct fields
- [x] Each domain package registers its own type via `init()`

**nudgenik.Result:**

```go
type Result struct {
    State      string   `json:"state" required:"true"`
    Confidence string   `json:"confidence,omitempty" required:"true"`
    Evidence   []string `json:"evidence,omitempty" required:"true" nullable:"false"`
    Summary    string   `json:"summary" required:"true"`
    Source     string   `json:"source,omitempty"` // excluded from schema
    _          struct{} `additionalProperties:"false"`
}
```

**branchsuggest.Result:**

```go
type Result struct {
    Branch   string   `json:"branch" required:"true"`
    Nickname string   `json:"nickname" required:"true"`
    _        struct{} `additionalProperties:"false"`
}
```

**conflictresolve types:**

```go
type FileAction struct {
    Action      string   `json:"action" required:"true"`
    Description string   `json:"description" required:"true"`
    _           struct{} `additionalProperties:"false"`
}

type OneshotResult struct {
    AllResolved bool                  `json:"all_resolved" required:"true"`
    Confidence  string                `json:"confidence" required:"true"`
    Summary     string                `json:"summary" required:"true"`
    Files       map[string]FileAction `json:"files" required:"true" nullable:"false"`
    _           struct{}              `additionalProperties:"false"`
}
```

### Phase 3: Schema Registry

- [x] `internal/schema/schema.go` provides `Register()`, `Get()`, `Labels()` API
- [x] Domain packages register their types in `init()`
- [x] Schemas are cached after first generation

### Phase 4: Update Oneshot Package

- [x] Removed `schemaRegistry` map with inline strings
- [x] Re-exported constants from `schema` package for backwards compatibility
- [x] Updated `resolveSchema()` to use `schema.Get()`
- [x] Updated `WriteAllSchemas()` to iterate `schema.Labels()`

### Phase 5: Validation and Testing

- [x] Verified generated schemas match current inline schemas (structurally equivalent)
- [x] `TestSchemaRegistry` validation passes (moved to external test package)
- [x] All existing tests pass

## Verification

Compare generated schema output against current inline strings to ensure equivalence:

```go
func TestGeneratedSchemaMatchesInline(t *testing.T) {
    generated := schema.ToJSON(schema.NudgeNik)
    expected := `{"type":"object","properties":{"state":{"type":"string"},...}`
    // Compare normalized JSON
}
```

## Rollback Plan

Keep the old `schemaRegistry` strings commented out until the new approach is validated in production. If issues arise, revert the import and uncomment the strings.

## References

- [swaggest/jsonschema-go](https://pkg.go.dev/github.com/swaggest/jsonschema-go)
- [google/jsonschema-go](https://pkg.go.dev/github.com/google/jsonschema-go/jsonschema)
- [OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)
- [OpenAI optional fields issue](https://community.openai.com/t/need-help-with-conditional-optional-fields-in-openai-json-schema-with-strict-true/1354794)
- Current implementation: `internal/oneshot/oneshot.go:27-31`
