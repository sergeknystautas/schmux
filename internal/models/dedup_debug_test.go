package models

import (
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

func TestDebugDedup(t *testing.T) {
	data, err := os.ReadFile("../../tmp/models-dev-raw.json")
	if err != nil {
		t.Skipf("no raw registry: %v", err)
	}
	cutoff := time.Now().AddDate(0, -12, 0)
	registry, err := ParseRegistry(data, cutoff)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sort.Slice(registry, func(i, j int) bool { return registry[i].ID < registry[j].ID })
	for _, m := range registry {
		if m.Provider == "anthropic" {
			fmt.Printf("  %s\n", m.ID)
		}
	}
}
