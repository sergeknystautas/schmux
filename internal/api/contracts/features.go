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
	Personas      bool `json:"personas"`
	CommStyles    bool `json:"comm_styles"`
	Autolearn     bool `json:"autolearn"`
	FloorManager  bool `json:"floor_manager"`
	Timelapse     bool `json:"timelapse"`
}
