//go:build !norepofeed

package repofeed

import (
	"sync"
)

// ConsumerConfig configures the repofeed consumer.
type ConsumerConfig struct {
	OwnEmail string
}

// IntentEntry represents a single activity from another developer.
type IntentEntry struct {
	Developer      string
	DisplayName    string
	Intent         string
	Status         ActivityStatus
	Started        string
	Branches       []string
	SessionCount   int
	Agents         []string
	LastActiveDate string // v2 only
	WorkspaceID    string // v2 only
}

// Consumer fetches and merges developer activity files from the orphan branch,
// filtering out the local developer's own entries.
type Consumer struct {
	config ConsumerConfig
	mu     sync.RWMutex
	files  []*DeveloperFile
}

// NewConsumer creates a new Consumer.
func NewConsumer(cfg ConsumerConfig) *Consumer {
	return &Consumer{
		config: cfg,
	}
}

// UpdateFromFiles stores the fetched developer files, filtering out own email.
func (c *Consumer) UpdateFromFiles(files []*DeveloperFile) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var filtered []*DeveloperFile
	for _, f := range files {
		if f.Developer != c.config.OwnEmail {
			filtered = append(filtered, f)
		}
	}
	c.files = filtered
}

// GetIntentsForRepo returns other developers' activities for a given repo slug (v1 format).
func (c *Consumer) GetIntentsForRepo(slug string) []IntentEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var entries []IntentEntry
	for _, f := range c.files {
		// v1 files: repo-keyed activities
		repo, ok := f.Repos[slug]
		if !ok {
			continue
		}
		for _, act := range repo.Activities {
			entries = append(entries, IntentEntry{
				Developer:    f.Developer,
				DisplayName:  f.DisplayName,
				Intent:       act.Intent,
				Status:       act.Status,
				Started:      act.Started,
				Branches:     act.Branches,
				SessionCount: act.SessionCount,
				Agents:       act.Agents,
			})
		}
	}
	return entries
}

// GetAllIntents returns all developers' intents from both v1 and v2 files,
// normalized into a common IntentEntry format.
func (c *Consumer) GetAllIntents() []IntentEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var entries []IntentEntry
	for _, f := range c.files {
		if f.Version >= 2 && len(f.Intents) > 0 {
			// v2 files: flat intents array
			for _, intent := range f.Intents {
				entries = append(entries, IntentEntry{
					Developer:      f.Developer,
					DisplayName:    f.DisplayName,
					Intent:         intent.IntentText,
					Status:         intent.Status,
					Started:        intent.Started,
					LastActiveDate: intent.LastActiveDate,
					WorkspaceID:    intent.ID,
				})
			}
		} else {
			// v1 files: flatten repo-keyed activities
			for _, repo := range f.Repos {
				for _, act := range repo.Activities {
					entries = append(entries, IntentEntry{
						Developer:    f.Developer,
						DisplayName:  f.DisplayName,
						Intent:       act.Intent,
						Status:       act.Status,
						Started:      act.Started,
						Branches:     act.Branches,
						SessionCount: act.SessionCount,
						Agents:       act.Agents,
					})
				}
			}
		}
	}
	return entries
}

// GetAllRepoSlugs returns all repo slugs seen across all developer files.
func (c *Consumer) GetAllRepoSlugs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := make(map[string]bool)
	for _, f := range c.files {
		for slug := range f.Repos {
			seen[slug] = true
		}
	}

	slugs := make([]string, 0, len(seen))
	for slug := range seen {
		slugs = append(slugs, slug)
	}
	return slugs
}
