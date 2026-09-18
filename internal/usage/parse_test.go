package usage

import (
	"bufio"
	"os"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// These are the captured repository streams, not invented successful payloads.
func capturedReport(t *testing.T, name string, parse func([]byte) (contracts.UsageProviderInfo, bool)) contracts.UsageProviderInfo {
	t.Helper()
	file, err := os.Open("../../assets/dashboard/src/lib/chat/__fixtures__/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		if p, ok := parse(scanner.Bytes()); ok {
			return p
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("no quota report in %s", name)
	return contracts.UsageProviderInfo{}
}

func TestCapturedClaudeQuota(t *testing.T) {
	p := capturedReport(t, "claude/permission-allow.jsonl", ParseClaudePlanUsage)
	if p.Status != "allowed" || len(p.Windows) != 2 {
		t.Fatalf("quota = %+v", p)
	}
	for i, want := range []struct {
		id      string
		percent float64
		reset   int64
	}{{"five_hour", 27, 1788606600}, {"seven_day", 11, 1788638400}} {
		got := p.Windows[i]
		if got.ID != want.id || got.UsedPercent == nil || *got.UsedPercent != want.percent || got.ResetsAt != want.reset {
			t.Fatalf("window = %+v, want %+v", got, want)
		}
	}
	if p.OverageDisabledReason != "org_level_disabled" || p.IsUsingOverage == nil || *p.IsUsingOverage {
		t.Fatalf("overage = %+v", p)
	}
}

func TestCapturedCodexQuota(t *testing.T) {
	p := capturedReport(t, "codex/stream.out.jsonl", ParseCodexPlanUsage)
	if p.PlanType != "prolite" || p.LimitID != "codex" || len(p.Windows) != 1 {
		t.Fatalf("quota = %+v", p)
	}
	w := p.Windows[0]
	if w.ID != "primary" || w.DurationMinutes != 10080 || w.UsedPercent == nil || *w.UsedPercent != 31 || w.ResetsAt != 1788748166 {
		t.Fatalf("weekly primary window = %+v", w)
	}
	if p.Credits == nil || p.Credits.Balance != "1552.1430437500" || !p.Credits.HasCredits || p.Credits.Unlimited {
		t.Fatalf("credits = %+v", p.Credits)
	}
}

func TestQuotaWindowsPreserveUnknownAndZero(t *testing.T) {
	for _, tt := range []struct {
		name, line string
		parse      func([]byte) (contracts.UsageProviderInfo, bool)
	}{
		{"claude", `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0},"seven_day":{"resetsAt":123}}}}`, ParseClaudePlanUsage},
		{"codex", `{"method":"account/rateLimits/updated","params":{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":300},"secondary":{"windowDurationMins":10080,"resetsAt":123}}}}`, ParseCodexPlanUsage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := tt.parse([]byte(tt.line))
			if !ok || len(p.Windows) != 2 {
				t.Fatalf("quota = %+v, ok=%v", p, ok)
			}
			if p.Windows[0].UsedPercent == nil || *p.Windows[0].UsedPercent != 0 || p.Windows[1].UsedPercent != nil {
				t.Fatalf("zero and absent percentages conflated: %+v", p.Windows)
			}
			if tt.name == "codex" && p.Windows[0].DurationMinutes != 300 {
				t.Fatalf("primary duration = %d", p.Windows[0].DurationMinutes)
			}
		})
	}
}

func TestClaudeActiveWindowOnly(t *testing.T) {
	p, ok := ParseClaudePlanUsage([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","rateLimitType":"seven_day_opus","resetsAt":123}}`))
	if !ok || len(p.Windows) != 1 || p.Windows[0].ID != "seven_day_opus" || p.Windows[0].UsedPercent != nil {
		t.Fatalf("status must not fabricate percent: %+v, ok=%v", p, ok)
	}
}

func TestQuotaParsersIgnoreTokenUsage(t *testing.T) {
	for _, line := range []string{
		`{"type":"assistant","message":{"usage":{"input_tokens":10}}}`,
		`{"type":"result","modelUsage":{"claude":{"inputTokens":100}}}`,
		`{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"total":{"inputTokens":100},"last":{"inputTokens":10}}}}`,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
		`{"method":"account/rateLimits/updated","params":{"rateLimits":{}}}`,
		`not json`,
	} {
		if p, ok := ParseClaudePlanUsage([]byte(line)); ok {
			t.Fatalf("Claude accepted %s: %+v", line, p)
		}
		if p, ok := ParseCodexPlanUsage([]byte(line)); ok {
			t.Fatalf("Codex accepted %s: %+v", line, p)
		}
	}
}
