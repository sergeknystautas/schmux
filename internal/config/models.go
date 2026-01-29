package config

import (
	"github.com/sergeknystautas/schmux/internal/detect"
)

// GetAvailableModels returns the available models from the detect package.
func (c *Config) GetAvailableModels(detected []detect.Tool) []detect.Model {
	return detect.GetAvailableModels(detected)
}
