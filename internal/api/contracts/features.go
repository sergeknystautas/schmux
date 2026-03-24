package contracts

// Features reports which optional modules are available in this build.
type Features struct {
	Tunnel bool `json:"tunnel"`
	GitHub bool `json:"github"`
}
