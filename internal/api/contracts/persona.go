package contracts

// Persona represents a behavioral profile for an agent.
type Persona struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Color        string `json:"color"`
	Prompt       string `json:"prompt"`
	Expectations string `json:"expectations"`
	BuiltIn      bool   `json:"built_in"`
}

// PersonaCreateRequest is the body for POST /api/personas.
type PersonaCreateRequest struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	Color        string `json:"color"`
	Prompt       string `json:"prompt"`
	Expectations string `json:"expectations,omitempty"`
}

// PersonaUpdateRequest is the body for PUT /api/personas/{id}.
type PersonaUpdateRequest struct {
	Name         *string `json:"name,omitempty"`
	Icon         *string `json:"icon,omitempty"`
	Color        *string `json:"color,omitempty"`
	Prompt       *string `json:"prompt,omitempty"`
	Expectations *string `json:"expectations,omitempty"`
}

// PersonaListResponse is the body for GET /api/personas.
type PersonaListResponse struct {
	Personas []Persona `json:"personas"`
}
