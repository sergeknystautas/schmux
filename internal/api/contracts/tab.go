package contracts

// Tab represents an accessory tab in a workspace.
type Tab struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Label     string            `json:"label"`
	Route     string            `json:"route"`
	Closable  bool              `json:"closable"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt string            `json:"created_at"`
}
