package fence

import (
	"sort"
	"strconv"
	"strings"
)

// bq is a single backtick, used to splice markdown code spans into the
// double-quoted capability-doc strings below. (Go raw strings cannot contain
// backticks, so the authored prose is built with concatenation instead.)
const bq = "`"

// RenderCapabilities renders the fence-capabilities.md document the
// fence-analyze endpoint writes for the analysis agent at analyze time. The
// catalog half is generated from the presets map and baselineDomains so it
// cannot drift from the code; the guidance half is authored prose kept beside
// the presets map so the two change together. The output is pure markdown with
// no absolute paths — the analyze prompt carries the exact monitor.log path.
func RenderCapabilities() string {
	var b strings.Builder
	b.WriteString("# Fence capabilities\n\n")
	b.WriteString(orientationText)

	// --- Generated catalog: derived from the presets map + baselineDomains ---
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)

	b.WriteString("## What the sandbox allows\n\n")
	b.WriteString("The grants below are facts about this fence configuration, generated from the " +
		"schmux source. They are the complete vocabulary a recommendation may draw on.\n\n")

	b.WriteString("### Presets\n\n")
	b.WriteString("There are exactly " + strconv.Itoa(len(names)) + " presets. Each is an identity " +
		"claim about the project; the grants follow from that identity.\n\n")
	for _, name := range names {
		writePresetGrants(&b, name, presets[name])
	}

	b.WriteString("### Baseline (applied to every fenced session)\n\n")
	b.WriteString("- Network domains always allowed: " + bq + strings.Join(baselineDomains, bq+", "+bq) + bq + ".\n\n")

	b.WriteString(policyLayeringText)

	b.WriteString("### Repo configuration schema (" + bq + "<repo>/.schmux/config.json" + bq + ")\n\n")
	b.WriteString("The repo may set exactly two fields under " + bq + "fence" + bq + ":\n\n")
	b.WriteString("- " + bq + "presets" + bq + ": array of preset names. Closed enum: " +
		bq + strings.Join(names, bq+", "+bq) + bq + ". Any value not in this list is silently ignored.\n")
	b.WriteString("- " + bq + "allowed_domains" + bq + ": array of hostnames. A full left-label " +
		"wildcard is permitted (" + bq + "*.domain.com" + bq + "); partial-label globs are rejected " +
		"and abort launch.\n\n")
	b.WriteString(closedWorldText)

	// --- Authored guidance ---
	b.WriteString(logGrammarText)
	b.WriteString(selectionText)
	b.WriteString(judgmentText)
	b.WriteString(outputContractText)

	return b.String()
}

// writePresetGrants renders one preset's concrete grants, derived from its
// struct fields so a field change is reflected here automatically.
func writePresetGrants(b *strings.Builder, name string, p preset) {
	b.WriteString("#### " + name + "\n\n")
	if len(p.cacheEnv) > 0 {
		b.WriteString("- Cache env redirects (each points at " + bq + "<workspace>/" + fenceCacheRel + "/<subdir>" + bq + "):\n")
		keys := make([]string, 0, len(p.cacheEnv))
		for k := range p.cacheEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("  - " + bq + k + bq + " -> " + bq + p.cacheEnv[k] + bq + "\n")
		}
	}
	if p.goFlags {
		b.WriteString("- Appends " + bq + "-modcacherw" + bq + " to " + bq + "GOFLAGS" + bq +
			" (keeps the Go module cache writable).\n")
	}
	if p.goTelemetry {
		b.WriteString("- Allows writing the Go telemetry directory.\n")
	}
	if p.allUnixSockets {
		b.WriteString("- Sets " + bq + "network.allowAllUnixSockets = true" + bq +
			" (permits any Unix-domain socket connection).\n")
	}
	if p.dockerConfig {
		b.WriteString("- Stages a " + bq + "DOCKER_CONFIG/config.json" + bq + " registering discovered " +
			"Docker CLI plugin directories so buildx/compose stay usable while fenced.\n")
	}
	if len(p.domains) > 0 {
		b.WriteString("- Adds network domains: " + bq + strings.Join(p.domains, bq+", "+bq) + bq + ".\n")
	}
	if len(p.machLookup) > 0 {
		b.WriteString("- Allows macOS Mach/XPC service lookups (" + bq + "macos.mach.lookup" + bq + "): " +
			bq + strings.Join(p.machLookup, bq+", "+bq) + bq + ".\n")
	}
	if len(p.machRegister) > 0 {
		b.WriteString("- Allows macOS Mach/XPC service registrations (" + bq + "macos.mach.register" + bq + "): " +
			bq + strings.Join(p.machRegister, bq+", "+bq) + bq + ".\n")
	}
	b.WriteString("\n")
}

const orientationText = "## You are inside a sandbox\n\n" +
	"Processes in this workspace run inside an OS sandbox called fence. Operations " +
	"it denies are logged to a per-session " + bq + "monitor.log" + bq + " (the prompt that pointed " +
	"you at this file gives the exact path). You may recommend changes only to " +
	bq + "<repo>/.schmux/config.json" + bq +
	" — specifically the two fields described below. You cannot change the sandbox itself, the baseline " +
	"policy, or anything outside that one file.\n\n"

const policyLayeringText = "### Policy layering (read-only context)\n\n" +
	"The effective sandbox policy composes, in order:\n" +
	"1. The fence " + bq + "code" + bq + " baseline template (network and filesystem defaults).\n" +
	"2. The selected presets above (cache redirects, GOFLAGS, unix sockets, docker config, preset domains, macOS Mach grants).\n" +
	"3. The repo's " + bq + "fence.allowed_domains" + bq + ".\n" +
	"4. schmux-added grants: write access to the workspace, and read access to this workspace's fence " +
	"directory (that is how you can read " + bq + "monitor.log" + bq + " and this doc).\n\n" +
	"Only layer 3 and the preset choice in layer 2 are things a repo may change. The baseline template " +
	"and schmux-added grants are fixed. Every denial in " + bq + "monitor.log" + bq + " comes from this " +
	"composed policy.\n\n"

const closedWorldText = "> **Closed world.** These are the only knobs. There are no other presets, no other " +
	"fields, and no escape hatches. A need that neither " + bq + "presets" + bq + " nor " +
	bq + "allowed_domains" + bq + " can express is a limitation of this sandbox's vocabulary — report it " +
	"in your Gaps section, do not improvise a workaround.\n\n"

const logGrammarText = "## How to read monitor.log\n\n" +
	"Every line has the shape:\n" +
	"    " + bq + "[fence:<channel>] <time> <mark> <message>" + bq + "\n" +
	"where " + bq + "<mark>" + bq + " is " + bq + "✗" + bq + " (denied) or " + bq + "✓" + bq + " (allowed). " +
	"You care about the " + bq + "✗" + bq + " lines.\n\n" +
	"- Channel " + bq + "http" + bq + ", message starting " + bq + "CONNECT <status> <domain> <url> (<dur>)" + bq +
	": a network connection attempt. A non-2xx status (e.g. 403) means the host was blocked. The domain is " +
	"exactly what " + bq + "fence.allowed_domains" + bq + " can open.\n" +
	"- Channel " + bq + "logstream" + bq + ", message starting " + bq + "file-" + bq + " (e.g. " +
	bq + "file-write-create <path> (<proc>:<pid>)" + bq + "): a filesystem operation denied. These are " +
	"usually the fence working correctly — the path is outside the workspace. Presets redirect caches into " +
	"the workspace so legitimate build writes succeed; a denied cache write often indicates a missing preset.\n" +
	"- Channel " + bq + "logstream" + bq + ", message starting " + bq + "mach-lookup <service> (<proc>:<pid>)" + bq +
	": a macOS system-service lookup denied. Most are incidental noise (telemetry, update checks, Apple " +
	"system services) — recommend ignoring those. The exceptions are in the selection rules below: " +
	bq + "org.chromium.*" + bq + " denials and AppKit-init failures in a project that launches a native " +
	"GUI map to the " + bq + "chromium" + bq + " and " + bq + "macos-gui" + bq + " presets.\n\n" +
	"There are denial shapes this doc does not enumerate. In particular, the exact line format of a " +
	"Unix-domain-socket denial is unverified (no in-repo example exists). When you see a " + bq + "✗" + bq +
	" line you do not recognize, describe it verbatim. Do not guess which knob it maps to.\n\n"

const selectionText = "## Choosing a recommendation\n\n" +
	"Presets are identity claims about the project, not capability toggles. Each preset's name asserts " +
	"\"this is a <X> project,\" and the grants follow from that identity. Recommend a preset only when the " +
	"denial evidence itself proves the identity:\n\n" +
	"- A denied write to a Go cache path proves Go tooling is running -> the " + bq + "golang" + bq +
	" preset is honest.\n" +
	"- npm/yarn/bun and pip/uv cache writes are already redirected into the workspace for every fenced " +
	"session; there is no node or python preset. A denial on one of those home-dir cache paths means a " +
	"tool bypassed the baseline redirect — report it in Gaps.\n" +
	"- A denied Mach registration or lookup of an " + bq + "org.chromium.*" + bq + " service proves a " +
	"Chromium browser is running -> the " + bq + "chromium" + bq + " preset is honest.\n" +
	"- A windowed app failing during AppKit init (denied lookups of window-server services such as " +
	bq + "com.apple.hiservices-xpcservice" + bq + " while the project launches a native GUI under test) " +
	"-> the " + bq + "macos-gui" + bq + " preset is honest. It disables Mach IPC isolation entirely, so " +
	"do not recommend it for anything less than a native GUI that must render.\n" +
	"- A denied Unix-socket connection proves nothing about tmux by itself -> " + bq + "tmux" + bq +
	" is dishonest unless the evidence shows the project actually drives tmux. Do not recommend " + bq +
	"tmux" + bq + " just because a socket was denied.\n" +
	"- " + bq + "docker" + bq + " is honest only when the evidence shows a Docker build or run in progress.\n\n" +
	bq + "allowed_domains" + bq + " is per-destination, not per-tool. Recommend the specific blocked domain, " +
	"taken verbatim from the " + bq + "CONNECT" + bq + " line. Do not broaden it.\n\n"

const judgmentText = "## Judgment: not every denial deserves an exception\n\n" +
	"Denials are the fence doing its job. Before recommending anything, classify each denial:\n" +
	"- (a) Legitimate dev-loop need — a build/test pulling a real dependency, a real package-cache write -> " +
	"recommend the honest lever (preset or domain).\n" +
	"- (b) Incidental noise — " + bq + "mach-lookup com.apple.*" + bq + ", telemetry endpoints, update pings, " +
	"OS service lookups -> explicitly recommend doing nothing.\n" +
	"- (c) Suspicious — an unexpected host, an exfiltration-shaped connection, a write to a sensitive path -> " +
	"flag it and recommend against allowing it.\n\n" +
	"Most " + bq + "mach-lookup" + bq + " lines and many " + bq + "CONNECT" + bq + " lines are (b). " +
	"Recommending exceptions for noise defeats the sandbox.\n\n"

const outputContractText = "## Output contract\n\n" +
	"For every recommendation:\n" +
	"- Cite the specific " + bq + "monitor.log" + bq + " line(s) it resolves (quote the line).\n" +
	"- State the predicted effect (\"adding <domain> to " + bq + "fence.allowed_domains" + bq + " stops the " +
	bq + "CONNECT 403" + bq + " denials to <domain>\"; \"adding the <preset> preset redirects <cache> into " +
	"the workspace and stops the file-write denials to <home path>\").\n" +
	"- Present the resulting " + bq + "fence" + bq + " block of " + bq + "<repo>/.schmux/config.json" + bq + ".\n\n" +
	"If a denial reflects a need this vocabulary cannot express (for example a Unix-socket connection to a " +
	"specific socket, or a host a preset's identity does not fit), put it in a **Gaps** section — describe " +
	"the need and the evidence, and say explicitly it cannot be satisfied with the current knobs. Do not " +
	"improvise a workaround. Do not invent presets or fields.\n\n" +
	"Do not apply changes unless explicitly asked.\n"
