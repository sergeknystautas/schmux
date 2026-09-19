package usage

import (
	"math"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// Quota-only payloads from the read-only endpoint spike, 2026-09-18.
// No account identities, credentials, or optional billing details are retained.
const kimiQuotaFixture = `{"usage":{"limit":"100","used":"14","remaining":"86","resetTime":"2026-09-25T03:16:12.484198Z"},"usages":{"limit_5h":{"used_ratio":0,"reset_time":"2026-09-18T22:16:11Z"},"limit_7d":{"used_ratio":0,"reset_time":"2026-09-25T03:16:11Z"}},"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","used":"3","remaining":"97","resetTime":"2026-09-18T22:16:12.484198Z"}}]}`
const zaiQuotaFixture = `{"success":true,"code":200,"data":{"limits":[{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":12000,"currentValue":90,"remaining":11909,"percentage":1,"nextResetTime":1789774300250},{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":60000,"currentValue":43836,"remaining":16163,"percentage":73,"nextResetTime":1790056836983}]}}`
const minimaxQuotaFixture = `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","start_time":1789743600000,"end_time":1789761600000,"current_interval_total_count":0,"current_interval_usage_count":0,"current_interval_status":1,"current_interval_remaining_percent":84,"current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1789344000000,"weekly_end_time":1789948800000,"current_weekly_status":1,"current_weekly_remaining_percent":41},{"model_name":"video","current_interval_status":3,"current_interval_remaining_percent":100}]}`

func TestAPIQuotaParsers(t *testing.T) {
	tests := []struct {
		name    string
		parse   func([]byte) (contracts.UsageProviderInfo, error)
		body    string
		percent []float64
		minutes []int64
		resets  []int64
	}{
		{"Kimi counts override conflicting zero ratios", parseKimiQuota, kimiQuotaFixture, []float64{3, 14}, []int64{300, 10080}, []int64{1789769772, 1790306172}},
		// Captured during the CodexBar comparison: weekly usage was 30%, not
		// the zero reported by limit_7d.used_ratio. Keep counts and their resets together.
		{"Kimi captured 30 percent weekly usage is not replaced by zero", parseKimiQuota, `{"usage":{"limit":"100","used":"30","remaining":"70","resetTime":"2026-09-25T03:16:12.484198Z"},"usages":{"limit_5h":{"used_ratio":0,"reset_time":"2026-09-19T08:16:11Z"},"limit_7d":{"used_ratio":0,"reset_time":"2026-09-25T03:16:11Z"}},"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","used":"13","remaining":"87","resetTime":"2026-09-19T08:16:12.484198Z"}}]}`, []float64{13, 30}, []int64{300, 10080}, []int64{1789805772, 1790306172}},
		{"Kimi genuine zero counts are authoritative", parseKimiQuota, `{"usage":{"limit":100,"used":0},"usages":{"limit_7d":{"used_ratio":0.3}}}`, []float64{0}, []int64{10080}, []int64{0}},
		{"Kimi ratios remain usable without counts", parseKimiQuota, `{"usages":{"limit_5h":{"used_ratio":0},"limit_7d":{"used_ratio":0.3}}}`, []float64{0, 30}, []int64{300, 10080}, []int64{0, 0}},
		{"Kimi unusable counts fall back per window", parseKimiQuota, `{"usage":{"limit":0,"used":0},"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":100}}],"usages":{"limit_5h":{"used_ratio":0.13},"limit_7d":{"used_ratio":0.3}}}`, []float64{13, 30}, []int64{300, 10080}, []int64{0, 0}},
		{"Kimi legacy counts", parseKimiQuota, `{"usage":{"limit":"100","used":"14"},"limits":[{"window":{"duration":5,"timeUnit":"TIME_UNIT_HOUR"},"detail":{"limit":100,"remaining":97}}]}`, []float64{3, 14}, []int64{300, 10080}, []int64{0, 0}},
		{"Kimi unknown window does not acquire five-hour identity", parseKimiQuota, `{"limits":[{"window":{"duration":5,"timeUnit":"unknown"},"detail":{"limit":100,"used":20}}]}`, []float64{20}, []int64{0}, []int64{0}},
		{"Kimi monthly has no invented duration", parseKimiQuota, `{"usages":{"limit_month_total":{"used_ratio":0.42,"reset_time":"2026-09-25T03:16:11Z"}}}`, []float64{42}, []int64{0}, []int64{1790306171}},
		{"Kimi invalid ratio falls back to counts", parseKimiQuota, `{"usage":{"limit":100,"used":14},"usages":{"limit_7d":{"used_ratio":-1}}}`, []float64{14}, []int64{10080}, []int64{0}},
		{"zai seven-day count ignores other entries", parseZaiQuota, zaiQuotaFixture, []float64{43837.0 / 600}, []int64{10080}, []int64{1790056836}},
		{"MiniMax percent quotas with zero counts", parseMiniMaxQuota, minimaxQuotaFixture, []float64{16, 59}, []int64{300, 10080}, []int64{1789761600, 1789948800}},
		{"MiniMax legacy usage_count means remaining", parseMiniMaxQuota, `{"data":{"base_resp":{"status_code":0},"model_remains":[{"model_name":"MiniMax-M2.7","current_interval_total_count":"100","current_interval_usage_count":"80"}]}}`, []float64{20}, []int64{0}, []int64{0}},
		{"MiniMax unlimited weekly not a finite tranche", parseMiniMaxQuota, `{"model_remains":[{"model_name":"general","current_interval_remaining_percent":100,"current_weekly_status":3,"current_weekly_remaining_percent":100}]}`, []float64{0}, []int64{0}, []int64{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.parse([]byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Windows) != len(tt.percent) {
				t.Fatalf("windows=%+v; want %d", got.Windows, len(tt.percent))
			}
			for i, w := range got.Windows {
				if w.UsedPercent == nil || math.Abs(*w.UsedPercent-tt.percent[i]) > 1e-9 || w.DurationMinutes != tt.minutes[i] || w.ResetsAt != tt.resets[i] {
					t.Errorf("window[%d]=%+v, percent=%v; want percent=%v minutes=%d reset=%d", i, w, w.UsedPercent, tt.percent[i], tt.minutes[i], tt.resets[i])
				}
				if w.DurationMinutes == 0 && w.ID == "five_hour" {
					t.Errorf("unknown duration given five_hour identity: %+v", w)
				}
			}
		})
	}
}

func TestAPIQuotaRejectsMissingAndInvalidUsage(t *testing.T) {
	parsers := map[string]func([]byte) (contracts.UsageProviderInfo, error){
		"kimi": parseKimiQuota, "minimax": parseMiniMaxQuota,
		"zai": parseZaiQuota,
	}
	for name, parse := range parsers {
		for _, body := range []string{`{`, `{}`, `null`} {
			t.Run(name+body, func(t *testing.T) {
				if got, err := parse([]byte(body)); err == nil {
					t.Fatalf("accepted missing quota: %+v", got)
				}
			})
		}
	}
	for _, tt := range []struct{ name, body string }{
		{"kimi", `{"usage":{"limit":100}}`},
		{"kimi", `{"usage":{"limit":0,"used":0}}`},
		{"kimi", `{"usages":{"limit_5h":{"reset_time":"bad"}}}`},
		{"minimax", `{"base_resp":{"status_code":1004},"model_remains":[{"model_name":"general","current_interval_remaining_percent":80}]}`},
		{"minimax", `{"data":{"base_resp":{"status_code":1004},"model_remains":[{"model_name":"general","current_interval_remaining_percent":80}]}}`},
		{"minimax", `{"model_remains":[{"model_name":"general","current_interval_total_count":100}]}`},
		{"zai", `{"success":false,"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":20}]}}`},
		{"zai", `{"success":true,"code":200,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5}]}}`},
	} {
		t.Run(tt.name+tt.body, func(t *testing.T) {
			if got, err := parsers[tt.name]([]byte(tt.body)); err == nil {
				t.Fatalf("accepted invalid quota: %+v", got)
			}
		})
	}
}
