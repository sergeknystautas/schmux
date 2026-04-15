package contracts

// RemoteProfileResponse represents a remote profile in API responses.
type RemoteProfileResponse struct {
	ID                    string                `json:"id"`
	DisplayName           string                `json:"display_name"`
	VCS                   string                `json:"vcs"`
	WorkspacePath         string                `json:"workspace_path"`
	ConnectCommand        string                `json:"connect_command,omitempty"`
	ReconnectCommand      string                `json:"reconnect_command,omitempty"`
	ProvisionCommand      string                `json:"provision_command,omitempty"`
	HostnameRegex         string                `json:"hostname_regex,omitempty"`
	VSCodeCommandTemplate string                `json:"vscode_command_template,omitempty"`
	Flavors               []RemoteProfileFlavor `json:"flavors"`
	HostType              string                `json:"host_type,omitempty"` // "ephemeral" (default) | "persistent"
}

// RemoteFlavorResponse represents a remote flavor in API responses.
// DEPRECATED: kept for backward compatibility with existing API consumers.
type RemoteFlavorResponse struct {
	ID                    string `json:"id"`
	Flavor                string `json:"flavor"`
	DisplayName           string `json:"display_name"`
	VCS                   string `json:"vcs"`
	WorkspacePath         string `json:"workspace_path"`
	ConnectCommand        string `json:"connect_command,omitempty"`
	ReconnectCommand      string `json:"reconnect_command,omitempty"`
	ProvisionCommand      string `json:"provision_command,omitempty"`
	HostnameRegex         string `json:"hostname_regex,omitempty"`
	VSCodeCommandTemplate string `json:"vscode_command_template,omitempty"`
}

// RemoteHostResponse represents a remote host in API responses.
type RemoteHostResponse struct {
	ID                    string `json:"id"`
	ProfileID             string `json:"profile_id"`
	Flavor                string `json:"flavor"`
	DisplayName           string `json:"display_name,omitempty"`
	Hostname              string `json:"hostname"`
	UUID                  string `json:"uuid,omitempty"`
	Status                string `json:"status"`
	Provisioned           bool   `json:"provisioned"`
	VCS                   string `json:"vcs,omitempty"`
	ConnectedAt           string `json:"connected_at,omitempty"`
	ExpiresAt             string `json:"expires_at,omitempty"`
	ProvisioningSessionID string `json:"provisioning_session_id,omitempty"` // Local tmux session for interactive provisioning terminal
	HostType              string `json:"host_type,omitempty"`               // "ephemeral" | "persistent"
}

// RemoteProfileStatusResponse represents a profile with the status of all its hosts.
type RemoteProfileStatusResponse struct {
	Profile     RemoteProfileResponse   `json:"profile"`
	FlavorHosts []RemoteFlavorHostGroup `json:"flavor_hosts"`
}

// RemoteFlavorHostGroup groups hosts by flavor within a profile status response.
type RemoteFlavorHostGroup struct {
	Flavor string                 `json:"flavor"`
	Hosts  []RemoteHostStatusItem `json:"hosts"`
}

// RemoteFlavorStatusResponse is kept for backward compatibility.
// DEPRECATED: Use RemoteProfileStatusResponse instead.
type RemoteFlavorStatusResponse = RemoteProfileStatusResponse

// RemoteHostStatusItem represents the status of a single remote host within a flavor.
type RemoteHostStatusItem struct {
	HostID    string `json:"host_id"`
	Hostname  string `json:"hostname"`
	Status    string `json:"status"`
	Connected bool   `json:"connected"`
}
