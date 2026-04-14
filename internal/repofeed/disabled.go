//go:build norepofeed

package repofeed

import (
	"context"
	"time"

	"github.com/sergeknystautas/schmux/internal/events"
)

type ActivityStatus string

const (
	StatusActive    ActivityStatus = "active"
	StatusInactive  ActivityStatus = "inactive"
	StatusCompleted ActivityStatus = "completed"
)

func (s ActivityStatus) Valid() bool { return false }

type DeveloperFile struct {
	Developer   string                     `json:"developer"`
	DisplayName string                     `json:"display_name"`
	Updated     string                     `json:"updated"`
	Repos       map[string]*RepoActivities `json:"repos"`
}

type RepoActivities struct {
	Activities []Activity `json:"activities"`
}

type Activity struct {
	ID           string         `json:"id"`
	Intent       string         `json:"intent"`
	Status       ActivityStatus `json:"status"`
	Started      string         `json:"started"`
	Branches     []string       `json:"branches"`
	SessionCount int            `json:"session_count"`
	Agents       []string       `json:"agents"`
}

func RepoSlug(name string) string { return name }

type PublisherConfig struct {
	DeveloperEmail string
	DisplayName    string
	RepoResolver   func(sessionID string) (repoSlug string, branch string)
}

type Publisher struct{}

var _ events.EventHandler = (*Publisher)(nil)

func NewPublisher(_ PublisherConfig) *Publisher {
	return &Publisher{}
}

func (p *Publisher) HandleEvent(_ context.Context, _ string, _ events.RawEvent, _ []byte) {}

func (p *Publisher) GetCurrentState() *DeveloperFile {
	return &DeveloperFile{Repos: make(map[string]*RepoActivities)}
}

func (p *Publisher) GetLastPushedAt() time.Time  { return time.Time{} }
func (p *Publisher) SetLastPushedAt(_ time.Time) {}
func (p *Publisher) LockForPush() func()         { return func() {} }

type ConsumerConfig struct {
	OwnEmail string
}

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

type Consumer struct{}

func NewConsumer(_ ConsumerConfig) *Consumer {
	return &Consumer{}
}

func (c *Consumer) UpdateFromFiles(_ []*DeveloperFile) {}

func (c *Consumer) GetIntentsForRepo(_ string) []IntentEntry {
	return nil
}

func (c *Consumer) GetAllRepoSlugs() []string {
	return nil
}

type GitOps struct {
	BareDir string
	Branch  string
}

func (g *GitOps) WriteDevFile(_ string, _ *DeveloperFile) error { return nil }
func (g *GitOps) ReadAllDevFiles() ([]*DeveloperFile, error)    { return nil, nil }
func (g *GitOps) PushToRemote(_ string) error                   { return nil }
func (g *GitOps) FetchFromRemote(_ string) error                { return nil }

func GitDirFromWorkDir(workDir string) string { return workDir + "/.git" }

func CleanupStaleIndexFiles(_ string) {}

func IsAvailable() bool { return false }
