package usage

import (
	"encoding/json"
	"sort"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// ParseClaudePlanUsage reads subscription quota updates, including allowed status.
// Message and result token counts are not plan utilization.
func ParseClaudePlanUsage(line []byte) (contracts.UsageProviderInfo, bool) {
	type window struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    int64    `json:"resetsAt"`
	}
	var v struct {
		Type string `json:"type"`
		Info *struct {
			Status                string            `json:"status"`
			RateLimitType         string            `json:"rateLimitType"`
			Utilization           *float64          `json:"utilization"`
			ResetsAt              int64             `json:"resetsAt"`
			UnifiedWindows        map[string]window `json:"unifiedWindows"`
			OverageStatus         string            `json:"overageStatus"`
			OverageDisabledReason string            `json:"overageDisabledReason"`
			IsUsingOverage        *bool             `json:"isUsingOverage"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(line, &v) != nil || v.Type != "rate_limit_event" || v.Info == nil {
		return contracts.UsageProviderInfo{}, false
	}
	info := v.Info
	if len(info.UnifiedWindows) == 0 && info.RateLimitType == "" && info.Status == "" {
		return contracts.UsageProviderInfo{}, false
	}
	result := contracts.UsageProviderInfo{
		Windows: []contracts.UsageWindow{}, Status: info.Status,
		OverageStatus: info.OverageStatus, OverageDisabledReason: info.OverageDisabledReason,
		IsUsingOverage: info.IsUsingOverage,
	}
	add := func(id string, w window) {
		var percent *float64
		if w.Utilization != nil {
			value := *w.Utilization * 100
			percent = &value
		}
		result.Windows = append(result.Windows, contracts.UsageWindow{ID: id, UsedPercent: percent, ResetsAt: w.ResetsAt})
	}
	for id, w := range info.UnifiedWindows {
		add(id, w)
	}
	// Some reports describe only the active window. Never fabricate a percentage
	// from allowed/rejected status or overwrite a fuller window.
	if _, exists := info.UnifiedWindows[info.RateLimitType]; !exists && info.RateLimitType != "" {
		add(info.RateLimitType, window{info.Utilization, info.ResetsAt})
	}
	sort.Slice(result.Windows, func(i, j int) bool { return result.Windows[i].ID < result.Windows[j].ID })
	return result, true
}

// ParseCodexPlanUsage reads the reported account quota, not thread token usage.
func ParseCodexPlanUsage(line []byte) (contracts.UsageProviderInfo, bool) {
	type window struct {
		UsedPercent        *float64 `json:"usedPercent"`
		WindowDurationMins int64    `json:"windowDurationMins"`
		ResetsAt           int64    `json:"resetsAt"`
	}
	var v struct {
		Method string `json:"method"`
		Params struct {
			Limits *struct {
				LimitID   string  `json:"limitId"`
				LimitName string  `json:"limitName"`
				PlanType  string  `json:"planType"`
				Primary   *window `json:"primary"`
				Secondary *window `json:"secondary"`
				Credits   *struct {
					HasCredits bool   `json:"hasCredits"`
					Unlimited  bool   `json:"unlimited"`
					Balance    string `json:"balance"`
				} `json:"credits"`
			} `json:"rateLimits"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &v) != nil || v.Method != "account/rateLimits/updated" || v.Params.Limits == nil {
		return contracts.UsageProviderInfo{}, false
	}
	info := v.Params.Limits
	if info.Primary == nil && info.Secondary == nil && info.Credits == nil && info.PlanType == "" {
		return contracts.UsageProviderInfo{}, false
	}
	result := contracts.UsageProviderInfo{
		Windows: []contracts.UsageWindow{}, PlanType: info.PlanType, LimitID: info.LimitID, LimitName: info.LimitName,
	}
	for _, slot := range []struct {
		id string
		w  *window
	}{{"primary", info.Primary}, {"secondary", info.Secondary}} {
		if slot.w != nil {
			result.Windows = append(result.Windows, contracts.UsageWindow{ID: slot.id, UsedPercent: slot.w.UsedPercent, DurationMinutes: slot.w.WindowDurationMins, ResetsAt: slot.w.ResetsAt})
		}
	}
	if info.Credits != nil {
		result.Credits = &contracts.UsageCredits{HasCredits: info.Credits.HasCredits, Unlimited: info.Credits.Unlimited, Balance: info.Credits.Balance}
	}
	return result, true
}
