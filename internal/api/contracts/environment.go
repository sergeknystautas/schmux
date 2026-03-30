package contracts

// EnvironmentVar represents a single environment variable comparison result.
type EnvironmentVar struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

// EnvironmentResponse is the response from GET /api/environment.
type EnvironmentResponse struct {
	Vars    []EnvironmentVar `json:"vars"`
	Blocked []string         `json:"blocked"`
}
