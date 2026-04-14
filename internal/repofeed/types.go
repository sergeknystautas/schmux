//go:build !norepofeed

package repofeed

// ActivityStatus represents the state of a developer's activity.
type ActivityStatus string

const (
	StatusActive    ActivityStatus = "active"
	StatusInactive  ActivityStatus = "inactive"
	StatusCompleted ActivityStatus = "completed"
)

// Valid returns true if the status is a known value.
func (s ActivityStatus) Valid() bool {
	switch s {
	case StatusActive, StatusInactive, StatusCompleted:
		return true
	}
	return false
}

// DeveloperFile is the per-developer JSON file stored on the dev-repofeed branch.
// Version 1 (default): uses Repos map for repo-keyed activities.
// Version 2: uses flat Intents array for workspace-level intent broadcasting.
type DeveloperFile struct {
	Version     int                        `json:"version,omitempty"`
	Developer   string                     `json:"developer"`
	DisplayName string                     `json:"display_name"`
	Updated     string                     `json:"updated"`
	Repos       map[string]*RepoActivities `json:"repos,omitempty"`
	Intents     []Intent                   `json:"intents,omitempty"`
}

// Intent represents a workspace-level development intent (v2 format).
type Intent struct {
	ID             string         `json:"id"`
	IntentText     string         `json:"intent"`
	Status         ActivityStatus `json:"status"`
	LastActiveDate string         `json:"last_active_date"`
	Started        string         `json:"started"`
}

// RepoActivities holds the activities for a single repo.
type RepoActivities struct {
	Activities []Activity `json:"activities"`
}

// Activity represents a single in-progress development intent.
type Activity struct {
	ID           string         `json:"id"`
	Intent       string         `json:"intent"`
	Status       ActivityStatus `json:"status"`
	Started      string         `json:"started"`
	Branches     []string       `json:"branches"`
	SessionCount int            `json:"session_count"`
	Agents       []string       `json:"agents"`
}

// RepoSlug converts a repo name to a URL-safe slug.
func RepoSlug(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}

// IsAvailable reports whether the repofeed module is included in this build.
func IsAvailable() bool { return true }
