// Package detectorfix is the synthetic detector-contract fixture for
// scripts/determinism.sh --verify-detector. It lives under testdata/, so
// `go test ./...` and every ./...-based tool skip it entirely; the harness
// runs it by explicit path and controls outcomes through environment
// variables — never through randomness.
//
// Contract (documented in docs/dev/determinism.md):
//   - DETECTOR_SAMPLE_INDEX — 1-based sample number within a configuration.
//   - DETECTOR_CONFIG       — name of the configuration being sampled.
package detectorfix

import (
	"os"
	"testing"
)

// TestDetectorAlternates passes on odd samples and fails on even samples,
// so two samples inside one configuration must classify as FLAKY.
func TestDetectorAlternates(t *testing.T) {
	if os.Getenv("DETECTOR_SAMPLE_INDEX") == "2" {
		t.Fatal("detector fixture: even sample fails by contract")
	}
}

// TestDetectorConfigBound passes in every configuration except cpu1, so
// base passes and cpu1 fails consistently: CONFIG-SENSITIVE, never FLAKY.
func TestDetectorConfigBound(t *testing.T) {
	if os.Getenv("DETECTOR_CONFIG") == "cpu1" {
		t.Fatal("detector fixture: cpu1 fails by contract")
	}
}

// TestDetectorStable always passes and must never appear in verdict.tsv.
func TestDetectorStable(t *testing.T) {}
