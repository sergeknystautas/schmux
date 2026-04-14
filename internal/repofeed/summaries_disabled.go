//go:build norepofeed

package repofeed

import "time"

type SummaryEntry struct {
	Summary        string    `json:"summary"`
	PromptsHash    string    `json:"prompts_hash"`
	LastSummarized time.Time `json:"last_summarized"`
}

type SummaryCache struct{}

func NewSummaryCache() *SummaryCache                   { return &SummaryCache{} }
func (sc *SummaryCache) Get(_ string) *SummaryEntry    { return nil }
func (sc *SummaryCache) Set(_ string, _ *SummaryEntry) {}
func (sc *SummaryCache) AllKeys() []string             { return nil }
func (sc *SummaryCache) Remove(_ string)               {}
