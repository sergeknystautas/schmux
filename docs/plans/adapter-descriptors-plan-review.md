VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is well-structured and covers most of the design spec, but has two critical issues that will cause compilation failures and one that will cause test failures. The go:embed directives will not compile when directories contain no matching .yaml files, and the `registerToolName` approach silently mutates a slice that callers like `IsBuiltinToolName` do not expect to grow, while breaking the existing `TestAllAdaptersRegistered` test.

## Critical Issues (must fix)

### C1. `go:embed` with `*.yaml` pattern fails on empty directories

The plan's Step 6 (loader.go) declares:

```go
//go:embed descriptors/*.yaml
var embeddedDescriptors embed.FS

//go:embed contrib/*.yaml
var embeddedContrib embed.FS
```

Go requires embed patterns to match at least one file. Both `descriptors/` and `contrib/` will initially contain zero `.yaml` files (only `.gitkeep` in `contrib/`). This causes a hard compilation error:

```
pattern descriptors/*.yaml: no matching files found
pattern contrib/*.yaml: no matching files found
```

I confirmed this empirically by creating a test directory with only `.gitkeep` and a `*.yaml` embed directive -- the build fails.

**Fix options:**

- (a) Use a build tag to conditionally include the embed directives only when files exist (complex).
- (b) Seed each directory with a no-op placeholder `.yaml` file (e.g., `_empty.yaml` containing `name: _placeholder`) and filter it out during loading.
- (c) Drop the glob and use `//go:embed descriptors` / `//go:embed contrib` to embed the whole directory (which works even when empty), then filter by extension at runtime. This is the approach `internal/config/defaults.go` uses with `//go:embed defaults`.
- (d) For `descriptors/` specifically: since Phase 1 does not move builtins to YAML, this directory serves no purpose yet. Remove the embed directive entirely for Phase 1 and add it in Phase 2 when actual files exist. For `contrib/`, option (c) is cleanest.

### C2. `registerToolName` silently mutates `builtinToolNames` -- breaks existing test and semantic contract

The plan's Step 7 appends descriptor names to `builtinToolNames`:

```go
func registerToolName(name string) {
    if !IsToolName(name) {
        builtinToolNames = append(builtinToolNames, name)
    }
}
```

This has three problems:

1. **Existing test breakage.** `TestAllAdaptersRegistered` in `adapter_test.go` asserts `len(adapters) != 4`. After Step 8 wires `LoadAndRegisterDescriptors` into daemon startup, any runtime descriptors in `~/.schmux/adapters/` on the dev machine will cause this test to fail. But even without that, tests in Step 7 themselves register "testool" into the global `adapters` map and never clean it up properly -- the save/restore in the test manipulates the local variable `adapters` but `AllAdapters()` reads the package-level `adapters`. Actually, the test does directly manipulate the package-level `adapters` map (since it's in the same package), but `registerToolName` also mutates `builtinToolNames` and the test does NOT save/restore that. So after `TestRegisterDescriptorAdapters` runs, `IsBuiltinToolName("testool")` returns true for the rest of the test process.

2. **Semantic confusion.** `IsBuiltinToolName` is used in config validation (`run_targets.go`) to prevent user-defined run targets from colliding with builtin tools. If descriptor adapters are added to `builtinToolNames`, a user could no longer create a run target named "orc" even though "orc" is not a builtin -- it came from a YAML descriptor. The name `IsBuiltinToolName` implies "hardcoded, shipped with the binary."

3. **Thread safety.** `builtinToolNames` is a slice that is read concurrently (e.g., during config validation, session resolution) and written to by `registerToolName`. No mutex protects it.

**Fix:** Create a separate `descriptorToolNames` slice (or use the existing `adapters` map), keep `IsBuiltinToolName` unchanged, and have `IsToolName` check both. The callers in `config.go`, `session/manager.go`, and `workspace/ensure/manager.go` that use `IsBuiltinToolName` should be audited to determine whether they should switch to `IsToolName` (most should). Save/restore both `adapters` and `builtinToolNames` in tests, or better yet, use a test-local registry.

### C3. `TestAllAdaptersRegistered` will fail when descriptors are loaded at test time

Even with C2 fixed, if `LoadAndRegisterDescriptors` is called during init/startup and tests run the full package, the hard-coded `len(adapters) != 4` assertion in `adapter_test.go:39` will fail. The plan does not mention updating this existing test. Since the embed directories are empty in Phase 1 and `LoadAndRegisterDescriptors` is called from the daemon (not from init), this may not actually fire during `go test ./internal/detect/`, but Step 7's test does register adapters and does not clean up `adapters` properly if test ordering causes `TestAllAdaptersRegistered` to run after `TestRegisterDescriptorAdapters` (Go runs tests in definition order within a file, but across files the order depends on file-level compilation). Since both tests are in the same package and Go runs tests of a package sequentially, the order matters.

**Fix:** The plan should include a task to update `TestAllAdaptersRegistered` to assert `>= 4` instead of `== 4`, or better, ensure test cleanup is complete.

## Suggestions (nice to have)

### S1. Step 4 task size is too large

Step 4 implements "all 20 `ToolAdapter` methods" for `GenericAdapter` plus `{model}` placeholder expansion, all in one step. The test alone is ~170 lines. This is closer to 15-20 minutes of work, not 2-5 minutes. Consider splitting into sub-steps: (4a) core methods (Name, Capabilities, ModelFlag, InstructionConfig), (4b) mode args (Interactive, Oneshot, Streaming with model expansion), (4c) signaling/persona, (4d) hooks delegation, (4e) skills/setup.

### S2. GenericAdapter.Detect() relies on package-private helpers but plan does not specify the implementation

The plan says:

> **`Detect(ctx)`**: iterate `d.Detect` entries, call existing helpers (`commandExists`, `fileExists`, `tryCommand`, `homebrewCaskInstalled`, `homebrewFormulaInstalled`, `npmGlobalInstalled`) based on entry type

These helpers are indeed package-private (lowercase) in `internal/detect/agents.go`, so they ARE accessible from `adapter_generic.go` since it's in the same package. This is correct. However, the plan provides no test for `Detect()`. The Step 4 tests only test argument building, signaling, persona, skills, etc. There should be at least a test for `Detect()` with a `path_lookup` entry for a known command (like `go` or `ls`) and an unknown command, to verify the detection loop works.

### S3. Missing test for unsupported mode errors

When a `GenericAdapter` has no `oneshot` config but `OneshotArgs` is called, what happens? The existing `GeminiAdapter` returns an error. The plan's implementation sketch just says "return `d.Oneshot.BaseArgs`" but doesn't specify error behavior when `d.Oneshot` is nil. The tests don't cover this case. A descriptor with only `capabilities: [interactive]` should return an error from `OneshotArgs`, matching how `GeminiAdapter` behaves.

### S4. `DisplayName` is parsed but never used

The descriptor has a `display_name` field and the plan tests for it, but `ToolAdapter` has no `DisplayName()` method. The `GenericAdapter` stores it but has no way to expose it. If `DisplayName` matters, add it to the interface or remove it from Phase 1.

### S5. The save/restore pattern in Step 7/8 tests is fragile

```go
origAdapters := make(map[string]ToolAdapter)
for k, v := range adapters {
    origAdapters[k] = v
}
defer func() {
    adapters = origAdapters
}()
```

This copies the map entries but if `registerAdapter` is called during the test, it modifies the package-level `adapters` map in place. The deferred restore replaces the map, which works. But `builtinToolNames` and `agentInstructionConfigs` are also mutated and not restored. Use `t.Cleanup` and restore all three.

### S6. Step 5 creates `internal/detect/descriptors/` as an empty directory

Empty directories cannot be committed to git (git tracks files, not directories). The plan does `git add internal/detect/descriptors` which will be a no-op. Either add a `.gitkeep` file or defer creating this directory to Phase 2.

### S7. Commit messages use `git commit` directly

The CLAUDE.md says "ALWAYS use `/commit` to create commits. NEVER run `git commit` directly." The plan shows raw `git commit -m` commands in every step. While this might be intentional for the plan format, an implementer following the plan literally would violate the pre-commit requirements.

### S8. `HookContext` missing fields in test

The plan's Step 3 test creates `HookContext{WorkspacePath: "/tmp/test"}` but `HookContext` also has `HooksDir`, `SessionID`, and `WorkspaceID` fields. While the `none` strategy ignores them, a more thorough test would populate all fields.

### S9. No validation that descriptor `name` doesn't conflict with reserved names

The plan validates against existing adapters at registration time, but a descriptor could use names like `default`, `auto`, `none` that might have semantic meaning elsewhere in the codebase. Consider a blocklist.

## Verified Claims (things confirmed are correct)

1. **`gopkg.in/yaml.v3` is already in `go.mod`** -- confirmed at line 19 of `go.mod`.
2. **`SignalingHooks` is iota=0 and `PersonaCLIFlag` is iota=0** -- confirmed in `adapter.go` lines 8-15 and 20-27.
3. **Detection helpers are package-private but accessible** -- `commandExists`, `fileExists`, `tryCommand`, `homebrewCaskInstalled`, `homebrewFormulaInstalled`, `npmGlobalInstalled` are all lowercase functions in `agents.go`, same package as where `adapter_generic.go` will live. No accessibility issue.
4. **`ToolAdapter` interface has 20 methods** -- confirmed by counting in `adapter.go` lines 46-120.
5. **`adapters` map, `registerAdapter`, `GetAdapter`, `AllAdapters` exist** -- confirmed in `adapter.go` lines 129-149.
6. **`builtinToolNames` and `agentInstructionConfigs` are package-level vars** -- confirmed in `tools.go` lines 4 and 25.
7. **`Model` and `RunnerSpec` types match what the test code uses** -- confirmed in `models.go`. `Model.Runners` is `map[string]RunnerSpec` and `RunnerSpec.ModelValue` is a string.
8. **File paths are accurate** -- `internal/detect/adapter.go`, `internal/detect/tools.go`, `internal/detect/agents.go`, `internal/daemon/daemon.go` all exist. The `contrib/` and `descriptors/` directories do not yet exist (correct, they are created in Step 5).
9. **Daemon startup uses `schmuxdir.Get()` for the config directory** -- confirmed in `daemon.go` line 344. The plan's `filepath.Join(configDir, "adapters")` approach is correct.
10. **Existing adapters register via `init()`** -- confirmed in `adapter_claude.go:13`, `adapter_codex.go:11`, `adapter_gemini.go:12`, `adapter_opencode.go:16`.
11. **Dependency groups are sound and avoid file conflicts** -- Steps 1/2/3 touch independent files, Step 4 depends on all three, Steps 5/6 are independent, etc. No two parallel steps touch the same file.
12. **Test commands are correct** -- `go test ./internal/detect/ -run "TestFoo" -count=1` is the right form for this codebase.
