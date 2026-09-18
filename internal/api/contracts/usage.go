package contracts

// UsageSnapshotResponse is the GET /api/usage payload.
type UsageSnapshotResponse struct {
	Providers []UsageProviderInfo `json:"providers"`
}

// UsageProviderInfo is the latest provider-reported plan quota snapshot.
// UpdatedAt is the receipt time, not an accounting-period boundary.
type UsageProviderInfo struct {
	Provider              string        `json:"provider"`
	UpdatedAt             string        `json:"updated_at"`
	Windows               []UsageWindow `json:"windows"`
	PlanType              string        `json:"plan_type,omitempty"`
	LimitID               string        `json:"limit_id,omitempty"`
	LimitName             string        `json:"limit_name,omitempty"`
	Status                string        `json:"status,omitempty"`
	OverageStatus         string        `json:"overage_status,omitempty"`
	OverageDisabledReason string        `json:"overage_disabled_reason,omitempty"`
	IsUsingOverage        *bool         `json:"is_using_overage,omitempty"`
	Credits               *UsageCredits `json:"credits,omitempty"`
}

// UsageWindow preserves the reported identity. Codex primary/secondary are
// slots, not durations: use DurationMinutes when present to label them.
type UsageWindow struct {
	ID              string   `json:"id"`
	UsedPercent     *float64 `json:"used_percent,omitempty"`
	DurationMinutes int64    `json:"duration_minutes,omitempty"`
	ResetsAt        int64    `json:"resets_at,omitempty"` // Unix seconds, as reported
}

// UsageCredits keeps the provider's balance without assuming currency.
type UsageCredits struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}
