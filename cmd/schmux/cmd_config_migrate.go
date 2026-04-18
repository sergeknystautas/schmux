package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// shellFeatureChars are substrings that signal the input cannot be flatly
// argv-split (per spec §4.1). If detected, runConfigMigrate exits with code 2
// and a manual-resolution message.
var shellFeatureChars = []string{"|", ">", "<", "&&", "||", ";", "`", "$("}

// migrateExitError wraps a migration error with the CLI exit code that should
// be returned. The dispatcher in main.go inspects this via errAsExitCode to
// pick the right os.Exit value.
type migrateExitError struct {
	exitCode int
	err      error
}

func (e *migrateExitError) Error() string { return e.err.Error() }
func (e *migrateExitError) Unwrap() error { return e.err }

// errExitCode returns the exit code carried by err, or 1 if err is non-nil
// without a wrapped exit code, or 0 if err is nil.
func errExitCode(err error) int {
	if err == nil {
		return 0
	}
	var me *migrateExitError
	if errors.As(err, &me) {
		return me.exitCode
	}
	return 1
}

// runConfigMigrate reads the config.json at the given path and converts any
// legacy string-form shell command values to argv-array form. Idempotent.
//
// On success and when there are changes, writes config.json.bak with the
// original contents and replaces the original at mode 0600 (per §2.2).
// With dryRun=true, prints the diff and exits without writing.
//
// Walks four schema sites (per spec §2.4):
//   - sapling_commands.*
//   - remote_profiles[].remote_vcs_commands.*  (nested in each profile entry)
//   - telemetry.command
//   - external_diff_commands[*].command
//
// Exit codes (returned via migrateExitError):
//
//	0 = success or no changes needed (returned as nil)
//	1 = parse error or write failure
//	2 = ambiguous shell-feature input that needs manual conversion
func runConfigMigrate(path string, dryRun bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Use json.Decoder with UseNumber to preserve numeric precision and the
	// decoded type to map[string]json.RawMessage so we can rewrite only the
	// command sites and preserve every other field byte-for-byte (well, after
	// re-marshalling).
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	changed := false

	// Site 1: sapling_commands.* (top-level map of {key → command}).
	if raw, ok := cfg["sapling_commands"]; ok {
		converted, didChange, err := migrateCommandsObject(raw, "sapling_commands")
		if err != nil {
			return err
		}
		if didChange {
			cfg["sapling_commands"] = converted
			changed = true
		}
	}

	// Site 2: remote_profiles[].remote_vcs_commands.* (nested map per profile).
	if raw, ok := cfg["remote_profiles"]; ok {
		converted, didChange, err := migrateRemoteProfiles(raw)
		if err != nil {
			return err
		}
		if didChange {
			cfg["remote_profiles"] = converted
			changed = true
		}
	}

	// Site 3: telemetry.command (nested single field).
	if raw, ok := cfg["telemetry"]; ok {
		converted, didChange, err := migrateNestedCommandField(raw, "command", "telemetry.command")
		if err != nil {
			return err
		}
		if didChange {
			cfg["telemetry"] = converted
			changed = true
		}
	}

	// Site 4: external_diff_commands[*].command (array of {name, command}).
	if raw, ok := cfg["external_diff_commands"]; ok {
		converted, didChange, err := migrateExternalDiffCommands(raw)
		if err != nil {
			return err
		}
		if didChange {
			cfg["external_diff_commands"] = converted
			changed = true
		}
	}

	if !changed {
		fmt.Println("config is already in argv form; nothing to migrate.")
		return nil
	}

	out, err := marshalConfig(cfg)
	if err != nil {
		return fmt.Errorf("marshal migrated config: %w", err)
	}

	// Print a unified-diff-style summary to stdout so the user can review.
	fmt.Println(unifiedDiff(string(data), string(out), path))

	if dryRun {
		fmt.Println("(dry-run; no files written)")
		return nil
	}

	bak := path + ".bak"
	// Backup at 0600 to align with §2.2 file mode tightening; overwrites any
	// existing backup so re-running the migrator after a botched edit still
	// captures the current on-disk state. We remove first because os.WriteFile
	// preserves the mode of an existing file rather than re-applying the
	// supplied perm bits.
	_ = os.Remove(bak)
	if err := os.WriteFile(bak, data, 0600); err != nil {
		return fmt.Errorf("write backup %s: %w", bak, err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// os.WriteFile keeps the existing mode if the file already existed; force
	// 0600 explicitly so a pristine config that started life at 0644 (e.g.
	// freshly created via `echo > config.json`) ends up tightened.
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	fmt.Printf("migrated %s (backup at %s)\n", path, bak)
	return nil
}

// marshalConfig re-emits the config map with deterministic key ordering and
// 2-space indentation so the output diff is stable across runs.
func marshalConfig(cfg map[string]json.RawMessage) ([]byte, error) {
	// json.MarshalIndent of a map[string]json.RawMessage already sorts keys
	// alphabetically, but we use a manual pass to preserve nicer formatting
	// for nested RawMessage values (which would otherwise be emitted on a
	// single line).
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		// Re-indent the value so nested structures look human-readable.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, cfg[k], "  ", "  "); err != nil {
			// Fall back to the original RawMessage if it isn't valid JSON
			// (shouldn't happen since we just unmarshalled it).
			pretty.Write(cfg[k])
		}
		fmt.Fprintf(&buf, "  %q: %s", k, pretty.String())
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// unifiedDiff returns a minimal change summary between old and new versions of
// the config. It is not a true unified diff (no hunk headers); it just prints
// each differing line prefixed with "-" or "+" so the user sees exactly which
// fields changed.
func unifiedDiff(oldText, newText, path string) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "--- %s (legacy)\n", path)
	fmt.Fprintf(&buf, "+++ %s (migrated)\n", path)
	oldLines := strings.Split(strings.TrimRight(oldText, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")
	// Build a set of new lines for O(1) lookup, then walk old lines printing
	// removals; then walk new lines printing additions. This keeps things
	// dependency-free while still being readable for the typical case where a
	// few values flip.
	newSet := make(map[string]int, len(newLines))
	for _, l := range newLines {
		newSet[l]++
	}
	oldSet := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		oldSet[l]++
	}
	for _, l := range oldLines {
		if newSet[l] == 0 {
			fmt.Fprintf(&buf, "- %s\n", l)
		}
	}
	for _, l := range newLines {
		if oldSet[l] == 0 {
			fmt.Fprintf(&buf, "+ %s\n", l)
		}
	}
	return buf.String()
}

// migrateCommandsObject walks a map of {key → command} and converts string
// values to argv arrays. Used by sapling_commands and the per-profile
// remote_vcs_commands map.
func migrateCommandsObject(raw json.RawMessage, contextLabel string) (json.RawMessage, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, false, fmt.Errorf("migrate %s: %w", contextLabel, err)
	}
	changed := false
	for k, v := range obj {
		converted, didChange, err := convertCommandValue(v, contextLabel+"."+k)
		if err != nil {
			return raw, false, err
		}
		if didChange {
			obj[k] = converted
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false, fmt.Errorf("marshal %s: %w", contextLabel, err)
	}
	return out, true, nil
}

// migrateRemoteProfiles walks the remote_profiles array and migrates each
// profile's nested remote_vcs_commands.* map.
func migrateRemoteProfiles(raw json.RawMessage) (json.RawMessage, bool, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return raw, false, fmt.Errorf("migrate remote_profiles: %w", err)
	}
	changed := false
	for i, elem := range arr {
		var profile map[string]json.RawMessage
		if err := json.Unmarshal(elem, &profile); err != nil {
			return raw, false, fmt.Errorf("migrate remote_profiles[%d]: %w", i, err)
		}
		rvcs, ok := profile["remote_vcs_commands"]
		if !ok {
			continue
		}
		label := fmt.Sprintf("remote_profiles[%d].remote_vcs_commands", i)
		converted, didChange, err := migrateCommandsObject(rvcs, label)
		if err != nil {
			return raw, false, err
		}
		if !didChange {
			continue
		}
		profile["remote_vcs_commands"] = converted
		newElem, err := json.Marshal(profile)
		if err != nil {
			return raw, false, fmt.Errorf("marshal remote_profiles[%d]: %w", i, err)
		}
		arr[i] = newElem
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return raw, false, fmt.Errorf("marshal remote_profiles: %w", err)
	}
	return out, true, nil
}

// migrateNestedCommandField looks for a named field in an object and converts
// its string value to argv form. Used for telemetry.command and as a helper
// for external_diff_commands[*].command.
func migrateNestedCommandField(raw json.RawMessage, fieldName, contextLabel string) (json.RawMessage, bool, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, false, fmt.Errorf("migrate %s: %w", contextLabel, err)
	}
	v, ok := obj[fieldName]
	if !ok {
		return raw, false, nil
	}
	converted, didChange, err := convertCommandValue(v, contextLabel)
	if err != nil {
		return raw, false, err
	}
	if !didChange {
		return raw, false, nil
	}
	obj[fieldName] = converted
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false, fmt.Errorf("marshal %s: %w", contextLabel, err)
	}
	return out, true, nil
}

// migrateExternalDiffCommands walks an array of {name, command} structs and
// converts each element's "command" field.
func migrateExternalDiffCommands(raw json.RawMessage) (json.RawMessage, bool, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return raw, false, fmt.Errorf("migrate external_diff_commands: %w", err)
	}
	changed := false
	for i, elem := range arr {
		label := fmt.Sprintf("external_diff_commands[%d].command", i)
		converted, didChange, err := migrateNestedCommandField(elem, "command", label)
		if err != nil {
			return raw, false, err
		}
		if !didChange {
			continue
		}
		arr[i] = converted
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return raw, false, fmt.Errorf("marshal external_diff_commands: %w", err)
	}
	return out, true, nil
}

// convertCommandValue is the per-field conversion: array-form is left alone,
// string-form is shell-split (after shell-feature detection), other forms are
// left alone (e.g. null).
//
// contextLabel is included in error messages so the user knows which key
// triggered the failure.
func convertCommandValue(v json.RawMessage, contextLabel string) (json.RawMessage, bool, error) {
	// Try array form first — already migrated.
	var arr []string
	if err := json.Unmarshal(v, &arr); err == nil {
		return v, false, nil
	}
	// Try string form — legacy.
	var str string
	if err := json.Unmarshal(v, &str); err != nil {
		// Not a string and not an argv array — leave alone (could be null,
		// number, bool, etc. — none of those are valid command shapes but it
		// is not the migrator's job to police the schema).
		return v, false, nil
	}
	// Empty string is a no-op (the daemon would also treat it as "use default").
	if str == "" {
		return v, false, nil
	}
	// Refuse shell features (spec §4.1). Exit code 2 + manual-resolution hint.
	for _, ch := range shellFeatureChars {
		if strings.Contains(str, ch) {
			return v, false, &migrateExitError{
				exitCode: 2,
				err: fmt.Errorf(
					"key %q uses shell features (%q) that don't translate to a flat argv. "+
						"Convert it manually using the sh -c escape hatch (see docs/api.md), "+
						"then re-run the migrator",
					contextLabel, ch),
			}
		}
	}
	argv, err := shellutil.Split(str)
	if err != nil {
		return v, false, fmt.Errorf("key %q: %w", contextLabel, err)
	}
	if len(argv) == 0 {
		// shellutil.Split returned an empty slice (whitespace-only input);
		// leave the value alone rather than write a meaningless empty argv.
		return v, false, nil
	}
	out, err := json.Marshal(argv)
	if err != nil {
		return v, false, fmt.Errorf("marshal argv for %q: %w", contextLabel, err)
	}
	return out, true, nil
}
