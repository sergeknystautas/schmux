package contracts

// Features reports which optional modules are available in this build.
type Features struct {
	Tunnel        bool `json:"tunnel"`
	GitHub        bool `json:"github"`
	Telemetry     bool `json:"telemetry"`
	Update        bool `json:"update"`
	DashboardSX   bool `json:"dashboardsx"`
	ModelRegistry bool `json:"model_registry"`
	Repofeed      bool `json:"repofeed"`
	Subreddit     bool `json:"subreddit"`
}
