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
	Developer    string
	DisplayName  string
	Intent       string
	Status       ActivityStatus
	Started      string
	Branches     []string
	SessionCount int
	Agents       []string
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

// GetIntentsForRepo returns other developers' activities for a given repo slug.
func (c *Consumer) GetIntentsForRepo(slug string) []IntentEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var entries []IntentEntry
	for _, f := range c.files {
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
