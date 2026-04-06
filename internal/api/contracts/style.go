package contracts

// Style represents a communication style for an agent.
type Style struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Tagline string `json:"tagline"`
	Prompt  string `json:"prompt"`
	BuiltIn bool   `json:"built_in"`
}

// StyleCreateRequest is the body for POST /api/styles.
type StyleCreateRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon"`
	Tagline string `json:"tagline"`
	Prompt  string `json:"prompt"`
}

// StyleUpdateRequest is the body for PUT /api/styles/{id}.
type StyleUpdateRequest struct {
	Name    *string `json:"name,omitempty"`
	Icon    *string `json:"icon,omitempty"`
	Tagline *string `json:"tagline,omitempty"`
	Prompt  *string `json:"prompt,omitempty"`
}

// StyleListResponse is the body for GET /api/styles.
type StyleListResponse struct {
	Styles []Style `json:"styles"`
}
