package config

import (
	"fmt"
	"strings"
)

func (c *Config) GetOllamaEndpoint() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Ollama == nil {
		return ""
	}
	return strings.TrimSpace(c.Ollama.Endpoint)
}

func (c *Config) SetOllamaEndpoint(endpoint string) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	c.mu.Lock()
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		c.Ollama = nil
	} else {
		if c.Ollama == nil {
			c.Ollama = &OllamaConfig{}
		}
		c.Ollama.Endpoint = endpoint
	}
	c.mu.Unlock()
	return c.Save()
}
