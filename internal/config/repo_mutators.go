package config

import "fmt"

// Repo mutators for the GitHub connect flow. Each mutates under the config
// mutex, invalidates the repo-URL cache, and saves — unlike handler-level
// Reload()+Save(), concurrent writers cannot interleave between the read
// and the write of the mutation itself.

// SetRepoURL rewrites the URL of the repo named name and saves the config.
// Name and bare_path are preserved.
func (c *Config) SetRepoURL(name, url string) error {
	if url == "" {
		return fmt.Errorf("repo URL must not be empty")
	}
	c.mu.Lock()
	found := false
	for i := range c.Repos {
		if c.Repos[i].Name == name {
			c.Repos[i].URL = url
			found = true
			break
		}
	}
	c.mu.Unlock()
	if !found {
		return fmt.Errorf("repo %q not found in config", name)
	}
	c.invalidateRepoURLCache()
	return c.Save()
}

// RemoveRepo deletes the repo entry named name and saves the config.
func (c *Config) RemoveRepo(name string) error {
	c.mu.Lock()
	idx := -1
	for i := range c.Repos {
		if c.Repos[i].Name == name {
			idx = i
			break
		}
	}
	if idx >= 0 {
		c.Repos = append(c.Repos[:idx], c.Repos[idx+1:]...)
	}
	c.mu.Unlock()
	if idx < 0 {
		return fmt.Errorf("repo %q not found in config", name)
	}
	c.invalidateRepoURLCache()
	return c.Save()
}

// AddRepo appends a repo entry and saves the config. Fails on a duplicate name.
func (c *Config) AddRepo(repo Repo) error {
	if repo.Name == "" || repo.URL == "" {
		return fmt.Errorf("repo name and URL must not be empty")
	}
	c.mu.Lock()
	for _, r := range c.Repos {
		if r.Name == repo.Name {
			c.mu.Unlock()
			return fmt.Errorf("repo %q already exists in config", repo.Name)
		}
	}
	c.Repos = append(c.Repos, repo)
	c.mu.Unlock()
	c.invalidateRepoURLCache()
	return c.Save()
}

func (c *Config) invalidateRepoURLCache() {
	c.repoURLMu.Lock()
	c.repoURLCache = nil
	c.repoURLMu.Unlock()
}
