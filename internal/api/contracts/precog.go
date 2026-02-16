package contracts

// PrecogJobStatus represents the status of a precog analysis job.
type PrecogJobStatus struct {
	Status      string  `json:"status"`                 // "running", "completed", "failed"
	CurrentPass string  `json:"current_pass,omitempty"` // "A", "B", "C", "D", "E", "F"
	StartedAt   string  `json:"started_at"`
	CompletedAt *string `json:"completed_at,omitempty"`
	Error       *string `json:"error,omitempty"`
}

// PrecogStartResponse is returned when starting a precog analysis.
type PrecogStartResponse struct {
	Status string `json:"status"` // "started"
	JobID  string `json:"job_id"`
}

// RCM is the Repository Construction Model - the main output of precog analysis.
type RCM struct {
	RepoSummary       RCMRepoSummary    `json:"repo_summary"`
	RuntimeComponents []RCMComponent    `json:"runtime_components"`
	Entrypoints       []RCMEntrypoint   `json:"entrypoints"`
	Capabilities      []RCMCapability   `json:"capabilities"`
	Contracts         []RCMContract     `json:"contracts"`
	Clusters          []RCMCluster      `json:"clusters"`
	Couplings         []RCMCoupling     `json:"couplings"`
	DriftFindings     []RCMDriftFinding `json:"drift_findings"`
	Gravity           []RCMGravity      `json:"gravity"`
	Trajectory        []RCMTrajectory   `json:"trajectory"`
	Confidence        RCMConfidence     `json:"confidence"`
}

// RCMRepoSummary contains high-level repo information.
type RCMRepoSummary struct {
	Name             string   `json:"name"`
	AnalyzedAt       string   `json:"analyzed_at"`
	CommitHash       string   `json:"commit_hash"`
	SystemType       string   `json:"system_type"`
	PrimaryLanguages []string `json:"primary_languages"`
	LinesOfCode      int      `json:"lines_of_code"`
}

// RCMComponent represents a runtime component (service, worker, UI, etc).
type RCMComponent struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"` // "service", "worker", "ui", "library", "cli"
	Anchors []string `json:"anchors"`
}

// RCMEntrypoint represents an entry point into the system.
type RCMEntrypoint struct {
	Type   string `json:"type"` // "api", "worker", "ui", "event", "cli"
	Anchor string `json:"anchor"`
	Notes  string `json:"notes,omitempty"`
}

// RCMCapability represents a coherent domain function the system provides.
type RCMCapability struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Keywords    []string             `json:"keywords"`
	Anchors     RCMCapabilityAnchors `json:"anchors"`
}

// RCMCapabilityAnchors holds the code anchors for a capability.
type RCMCapabilityAnchors struct {
	Entrypoints []string `json:"entrypoints"`
	Modules     []string `json:"modules"`
	Schema      []string `json:"schema"`
	Symbols     []string `json:"symbols"`
}

// RCMContract represents a coordination surface that forces sequencing.
type RCMContract struct {
	ID                 string   `json:"id"`
	Type               string   `json:"type"` // "api_schema", "shared_model", "db_schema", "event", "config", "auth_policy", "library"
	Name               string   `json:"name"`
	Anchor             string   `json:"anchor"`
	UsedByCapabilities []string `json:"used_by_capabilities"`
	FanIn              int      `json:"fan_in"`
	Notes              string   `json:"notes,omitempty"`
}

// RCMCluster represents a group of tightly coupled files.
type RCMCluster struct {
	ID                   string   `json:"id"`
	Type                 string   `json:"type"` // "structural", "evolutionary", "hybrid"
	Name                 string   `json:"name"`
	Members              []string `json:"members"`
	CapabilitiesInvolved []string `json:"capabilities_involved"`
}

// RCMCoupling represents coupling between capabilities.
type RCMCoupling struct {
	CapabilityA string   `json:"capability_a"`
	CapabilityB string   `json:"capability_b"`
	Strength    string   `json:"strength"` // "high", "medium", "low"
	Evidence    []string `json:"evidence"`
}

// RCMDriftFinding represents a mismatch between intended and actual structure.
type RCMDriftFinding struct {
	ID                   string   `json:"id"`
	DeclaredBoundary     string   `json:"declared_boundary"`
	ObservedBehavior     string   `json:"observed_behavior"`
	ImpactOnParallelWork string   `json:"impact_on_parallel_work"`
	Anchors              []string `json:"anchors"`
}

// RCMGravity represents a region attracting development work.
type RCMGravity struct {
	Region      string   `json:"region"`
	Type        string   `json:"type"` // "capability", "contract"
	Signals     []string `json:"signals"`
	Implication string   `json:"implication"`
}

// RCMTrajectory represents a direction the system is evolving.
type RCMTrajectory struct {
	Direction  string   `json:"direction"`
	Evidence   []string `json:"evidence"`
	Confidence string   `json:"confidence"` // "high", "medium", "low"
}

// RCMConfidence holds confidence estimates for each section.
type RCMConfidence struct {
	Capabilities      string `json:"capabilities"`
	CapabilitiesNotes string `json:"capabilities_notes,omitempty"`
	Contracts         string `json:"contracts"`
	ContractsNotes    string `json:"contracts_notes,omitempty"`
	Clusters          string `json:"clusters"`
	ClustersNotes     string `json:"clusters_notes,omitempty"`
	Drift             string `json:"drift"`
	DriftNotes        string `json:"drift_notes,omitempty"`
	Trajectory        string `json:"trajectory"`
	TrajectoryNotes   string `json:"trajectory_notes,omitempty"`
}
