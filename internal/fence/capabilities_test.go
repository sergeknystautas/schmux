package fence

import (
	"sort"
	"strings"
	"testing"
)

// TestRenderCapabilities_EveryPresetAppears is the drift guard: every key in
// the presets map must surface in the rendered doc. Add a preset to the map and
// this test fails until the doc lists it — with no other change required.
func TestRenderCapabilities_EveryPresetAppears(t *testing.T) {
	doc := RenderCapabilities()
	for name := range presets {
		if !strings.Contains(doc, name) {
			t.Errorf("RenderCapabilities() doc missing preset %q", name)
		}
	}
}

func TestRenderCapabilities_PresetGrantsAndBaseline(t *testing.T) {
	doc := RenderCapabilities()

	// Each preset's concrete grants are rendered, derived from the struct fields.
	for name, p := range presets {
		t.Run("preset/"+name, func(t *testing.T) {
			for _, sub := range p.cacheEnv {
				if !strings.Contains(doc, sub) {
					t.Errorf("doc missing cache subdir %q for preset %q", sub, name)
				}
			}
			if p.goFlags && !strings.Contains(doc, "-modcacherw") {
				t.Errorf("doc missing GOFLAGS grant for preset %q", name)
			}
			if p.goTelemetry && !strings.Contains(doc, "telemetry") {
				t.Errorf("doc missing Go telemetry grant for preset %q", name)
			}
			if p.allUnixSockets && !strings.Contains(doc, "allowAllUnixSockets") {
				t.Errorf("doc missing allowAllUnixSockets grant for preset %q", name)
			}
			if p.dockerConfig && !strings.Contains(doc, "DOCKER_CONFIG") {
				t.Errorf("doc missing docker config grant for preset %q", name)
			}
			for _, d := range p.domains {
				if !strings.Contains(doc, d) {
					t.Errorf("doc missing domain %q for preset %q", d, name)
				}
			}
		})
	}

	// Baseline domains are listed.
	for _, d := range baselineDomains {
		if !strings.Contains(doc, d) {
			t.Errorf("doc missing baseline domain %q", d)
		}
	}
}

func TestRenderCapabilities_SchemaAndClosedWorld(t *testing.T) {
	doc := RenderCapabilities()

	// Both knobs are named.
	for _, want := range []string{"presets", "allowed_domains"} {
		if !strings.Contains(doc, want) {
			t.Errorf("doc missing knob %q", want)
		}
	}
	// Closed-world statement is present.
	if !strings.Contains(doc, "no other presets") {
		t.Errorf("doc missing closed-world statement")
	}

	// The schema's closed enum lists exactly the preset map keys.
	want := make([]string, 0, len(presets))
	for n := range presets {
		want = append(want, n)
	}
	sort.Strings(want)
	for _, n := range want {
		if !strings.Contains(doc, n) {
			t.Errorf("doc enum missing preset %q", n)
		}
	}
}

// TestRenderCapabilities_GuidanceSections verifies the authored guidance
// (orientation, log grammar, selection, judgment, output contract) rendered.
func TestRenderCapabilities_GuidanceSections(t *testing.T) {
	doc := RenderCapabilities()
	for _, want := range []string{
		"sandbox",        // orientation
		"CONNECT",        // log grammar: network
		"file-",          // log grammar: filesystem
		"mach-lookup",    // log grammar: system noise
		"identity claim", // selection semantics
		"Gaps",           // output contract
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("doc missing guidance marker %q", want)
		}
	}
}
