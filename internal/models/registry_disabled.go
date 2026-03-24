//go:build nomodelregistry

package models

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
)

const RegistryURL = ""

// RegistryModel is a model parsed from models.dev with bonus metadata.
type RegistryModel struct {
	ID            string
	DisplayName   string
	Provider      string
	ContextWindow int
	MaxOutput     int
	CostInput     float64
	CostOutput    float64
	Reasoning     bool
	ReleaseDate   string
}

// ParseRegistry is a no-op stub when the model registry is excluded.
func ParseRegistry(data []byte, cutoff time.Time) ([]RegistryModel, error) {
	return nil, nil
}

// BuildDetectModels is a no-op stub when the model registry is excluded.
func BuildDetectModels(registry []RegistryModel) []detect.Model {
	return nil
}

// LoadCache is a no-op stub when the model registry is excluded.
func LoadCache(schmuxDir string) ([]byte, error) {
	return nil, nil
}

// SaveCache is a no-op stub when the model registry is excluded.
func SaveCache(schmuxDir string, data []byte) error {
	return nil
}

// CachePath is a no-op stub when the model registry is excluded.
func CachePath(schmuxDir string) string {
	return ""
}

// RegistryCutoff is a no-op stub when the model registry is excluded.
func RegistryCutoff() time.Time {
	return time.Time{}
}

// NormalizeProvider returns the input unchanged when the model registry is excluded.
func NormalizeProvider(provider string) string {
	return provider
}

// IsAvailable reports whether the model registry module is included in this build.
func IsAvailable() bool { return false }
