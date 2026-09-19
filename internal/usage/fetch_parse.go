package usage

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// Endpoint and field semantics were verified against CodexBar's API fetchers
// and live provider responses. See docs/plan-usage.md for source links.
func parseKimiQuota(body []byte) (contracts.UsageProviderInfo, error) {
	type detail struct {
		Limit     *json.Number `json:"limit"`
		Used      *json.Number `json:"used"`
		Remaining *json.Number `json:"remaining"`
		ResetTime string       `json:"resetTime"`
	}
	type pool struct {
		UsedRatio *float64 `json:"used_ratio"`
		ResetTime string   `json:"reset_time"`
	}
	var response struct {
		Usage  *detail         `json:"usage"`
		Usages map[string]pool `json:"usages"`
		Limits []struct {
			Window struct {
				Duration int64  `json:"duration"`
				TimeUnit string `json:"timeUnit"`
			} `json:"window"`
			Detail detail `json:"detail"`
		} `json:"limits"`
	}
	if json.Unmarshal(body, &response) != nil {
		return contracts.UsageProviderInfo{}, fmt.Errorf("invalid Kimi quota response")
	}
	var windows []contracts.UsageWindow
	addDetail := func(id string, minutes int64, d detail) {
		total, valid := numeric(d.Limit)
		if !valid || total <= 0 {
			return
		}
		used, valid := numeric(d.Used)
		if !valid {
			remaining, ok := numeric(d.Remaining)
			if !ok || remaining < 0 || remaining > total {
				return
			}
			used = total - remaining
		}
		if used < 0 {
			return
		}
		windows = append(windows, contracts.UsageWindow{ID: id, UsedPercent: percentage(100 * used / total), DurationMinutes: minutes, ResetsAt: unixReset(d.ResetTime)})
	}
	// Prefer valid counts: Kimi can report zero-valued ratio pools alongside
	// nonzero usage counts. Ratios are a fallback, not an override of counts.
	// A monthly pool has no known fixed duration.
	for _, spec := range []struct {
		key, id string
		minutes int64
	}{
		{"limit_5h", "five_hour", 300}, {"limit_7d", "seven_day", 10080}, {"limit_month_total", "monthly", 0},
	} {
		before := len(windows)
		if spec.key == "limit_7d" && response.Usage != nil {
			addDetail(spec.id, spec.minutes, *response.Usage)
		}
		if spec.key == "limit_5h" {
			for i, limit := range response.Limits {
				multipliers := map[string]int64{"TIME_UNIT_MINUTE": 1, "TIME_UNIT_HOUR": 60, "TIME_UNIT_DAY": 1440}
				minutes := max(0, limit.Window.Duration) * multipliers[limit.Window.TimeUnit]
				// Do not give an unknown duration a five_hour id: the UI knows
				// that id's duration, which would manufacture a pace estimate.
				addDetail(fmt.Sprintf("limit_%d", i), minutes, limit.Detail)
			}
		}
		if len(windows) == before {
			p, exists := response.Usages[spec.key]
			if exists && p.UsedRatio != nil && *p.UsedRatio >= 0 {
				windows = append(windows, contracts.UsageWindow{ID: spec.id, UsedPercent: percentage(*p.UsedRatio * 100), DurationMinutes: spec.minutes, ResetsAt: unixReset(p.ResetTime)})
			}
		}
	}
	return quotaReport(windows)
}

func parseZaiQuota(body []byte) (contracts.UsageProviderInfo, error) {
	var response struct {
		Success bool `json:"success"`
		Code    int  `json:"code"`
		Data    struct {
			Limits []struct {
				Type       string   `json:"type"`
				Unit       int      `json:"unit"`
				Number     int64    `json:"number"`
				Percentage *float64 `json:"percentage"`
				Usage      *float64 `json:"usage"`
				Current    *float64 `json:"currentValue"`
				Remaining  *float64 `json:"remaining"`
				Reset      int64    `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || !response.Success || response.Code != 200 {
		return contracts.UsageProviderInfo{}, fmt.Errorf("invalid or unsuccessful z.ai quota response")
	}
	var windows []contracts.UsageWindow
	for _, limit := range response.Data.Limits {
		// TIME_LIMIT is the separate MCP quota, not the coding plan.
		if limit.Type != "TOKENS_LIMIT" && limit.Type != "CREDIT_LIMIT" {
			continue
		}
		// z.ai's coding-plan response has a seven-day quota (unit 6). Do not
		// assign a time horizon to its other entries.
		if limit.Unit != 6 {
			continue
		}
		percent := limit.Percentage
		if limit.Usage != nil && *limit.Usage > 0 {
			used := limit.Current
			if limit.Remaining != nil {
				v := *limit.Usage - *limit.Remaining
				if used != nil {
					v = max(v, *used)
				}
				used = &v
			}
			if used != nil {
				percent = percentage(*used / *limit.Usage * 100)
			}
		}
		if percent == nil {
			continue
		}
		windows = append(windows, contracts.UsageWindow{ID: "seven_day", UsedPercent: percentage(*percent), DurationMinutes: max(0, limit.Number) * 10080, ResetsAt: limit.Reset / 1000})
	}
	return quotaReport(windows)
}

func parseMiniMaxQuota(body []byte) (contracts.UsageProviderInfo, error) {
	type status struct {
		Code int `json:"status_code"`
	}
	type model struct {
		Name                   string       `json:"model_name"`
		Total                  *json.Number `json:"current_interval_total_count"`
		Remaining              *json.Number `json:"current_interval_usage_count"`
		RemainingPercent       *float64     `json:"current_interval_remaining_percent"`
		Status                 int          `json:"current_interval_status"`
		Start                  int64        `json:"start_time"`
		End                    int64        `json:"end_time"`
		WeeklyTotal            *json.Number `json:"current_weekly_total_count"`
		WeeklyRemaining        *json.Number `json:"current_weekly_usage_count"`
		WeeklyRemainingPercent *float64     `json:"current_weekly_remaining_percent"`
		WeeklyStatus           int          `json:"current_weekly_status"`
		WeeklyStart            int64        `json:"weekly_start_time"`
		WeeklyEnd              int64        `json:"weekly_end_time"`
	}
	type payload struct {
		Status *status `json:"base_resp"`
		Models []model `json:"model_remains"`
	}
	var response struct {
		payload
		Data *payload `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return contracts.UsageProviderInfo{}, fmt.Errorf("invalid MiniMax quota response")
	}
	data := response.payload
	if response.Data != nil {
		data = *response.Data
	}
	if response.Status != nil && response.Status.Code != 0 || data.Status != nil && data.Status.Code != 0 {
		return contracts.UsageProviderInfo{}, fmt.Errorf("unsuccessful MiniMax quota response")
	}
	var windows []contracts.UsageWindow
	add := func(id string, total, remaining *json.Number, remainingPercent *float64, status int, start, end int64) {
		// Status 3 denotes unavailable/unlimited lanes, not a finite tranche.
		if status == 3 {
			return
		}
		var percent *float64
		if remainingPercent != nil {
			percent = percentage(100 - *remainingPercent)
		} else {
			t, tok := numeric(total)
			r, rok := numeric(remaining)
			if tok && rok && t > 0 && r >= 0 {
				percent = percentage((t - r) / t * 100)
			}
		}
		if percent == nil {
			return
		}
		var minutes int64
		if start > 0 && end > start {
			minutes = (end - start) / 60000
		}
		windows = append(windows, contracts.UsageWindow{ID: id, UsedPercent: percent, DurationMinutes: minutes, ResetsAt: end / 1000})
	}
	for _, m := range data.Models {
		name := strings.ToLower(m.Name)
		if name != "general" && !strings.Contains(name, "minimax-m") && !strings.HasPrefix(name, "m2.") {
			continue
		}
		// Despite the field name, *_usage_count is REMAINING quota.
		add(m.Name+"_interval", m.Total, m.Remaining, m.RemainingPercent, m.Status, m.Start, m.End)
		add(m.Name+"_weekly", m.WeeklyTotal, m.WeeklyRemaining, m.WeeklyRemainingPercent, m.WeeklyStatus, m.WeeklyStart, m.WeeklyEnd)
	}
	return quotaReport(windows)
}
