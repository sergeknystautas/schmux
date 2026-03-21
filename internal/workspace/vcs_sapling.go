package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

type SaplingBackend struct {
	manager  *Manager
	commands config.SaplingCommands
}

var _ VCSBackend = (*SaplingBackend)(nil)

func NewSaplingBackend(m *Manager, cmds config.SaplingCommands) *SaplingBackend {
	return &SaplingBackend{manager: m, commands: cmds}
}

func renderCommandTemplate(tmpl string, vars map[string]string) (string, error) {
	t, err := template.New("cmd").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("invalid command template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}
	return buf.String(), nil
}

func (s *SaplingBackend) runTemplateCommand(ctx context.Context, tmpl string, vars map[string]string) ([]byte, error) {
	rendered, err := renderCommandTemplate(tmpl, vars)
	if err != nil {
		return nil, err
	}
	s.manager.logger.Info("running sapling command", "cmd", rendered)
	cmd := exec.CommandContext(ctx, "sh", "-c", rendered)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (s *SaplingBackend) EnsureRepoBase(ctx context.Context, repoIdentifier, basePath string) (string, error) {
	vars := map[string]string{
		"RepoIdentifier": repoIdentifier,
	}

	checkCmd := s.commands.CheckRepoBase
	if checkCmd != "" {
		output, err := s.runTemplateCommand(ctx, checkCmd, vars)
		if err == nil {
			path := strings.TrimSpace(string(output))
			if path != "" {
				s.manager.state.AddRepoBase(state.RepoBase{
					RepoURL: repoIdentifier,
					Path:    path,
					VCS:     "sapling",
				})
				s.manager.state.Save()
				return path, nil
			}
		}
	}

	if basePath == "" {
		repo, found := s.manager.config.FindRepoByURL(repoIdentifier)
		if found && repo.BarePath != "" {
			basePath = filepath.Join(s.manager.config.GetWorktreeBasePath(), repo.BarePath)
		} else if found {
			basePath = filepath.Join(s.manager.config.GetWorktreeBasePath(), repo.Name)
		} else {
			return "", fmt.Errorf("no base path configured for sapling repo: %s", repoIdentifier)
		}
	}

	if _, err := os.Stat(basePath); err == nil {
		s.manager.state.AddRepoBase(state.RepoBase{
			RepoURL: repoIdentifier,
			Path:    basePath,
			VCS:     "sapling",
		})
		s.manager.state.Save()
		return basePath, nil
	}

	if err := os.MkdirAll(filepath.Dir(basePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create parent directory: %w", err)
	}

	vars["BasePath"] = basePath
	createCmd := s.commands.GetCreateRepoBase()
	if _, err := s.runTemplateCommand(ctx, createCmd, vars); err != nil {
		return "", fmt.Errorf("create repo base failed: %w", err)
	}

	s.manager.state.AddRepoBase(state.RepoBase{
		RepoURL: repoIdentifier,
		Path:    basePath,
		VCS:     "sapling",
	})
	s.manager.state.Save()

	return basePath, nil
}

func (s *SaplingBackend) resolveRepoURL(repoBasePath string) string {
	for _, repo := range s.manager.config.GetRepos() {
		if repo.VCS != "sapling" {
			continue
		}
		candidatePath := filepath.Join(s.manager.config.GetWorktreeBasePath(), repo.BarePath)
		if candidatePath == repoBasePath {
			return repo.URL
		}
		if repo.BarePath != "" && strings.HasSuffix(repoBasePath, "/"+repo.BarePath) {
			return repo.URL
		}
	}
	for _, rb := range s.manager.state.GetRepoBases() {
		if rb.Path == repoBasePath && rb.VCS == "sapling" {
			return rb.RepoURL
		}
	}
	return repoBasePath
}

func (s *SaplingBackend) CreateWorkspace(ctx context.Context, repoBasePath, branch, destPath string) error {
	repoID := s.resolveRepoURL(repoBasePath)
	vars := map[string]string{
		"RepoIdentifier": repoID,
		"RepoBasePath":   repoBasePath,
		"Branch":         branch,
		"DestPath":       destPath,
	}
	cmd := s.commands.GetCreateWorkspace()
	if _, err := s.runTemplateCommand(ctx, cmd, vars); err != nil {
		return fmt.Errorf("create workspace failed: %w", err)
	}
	return nil
}

func (s *SaplingBackend) RemoveWorkspace(ctx context.Context, workspacePath string) error {
	vars := map[string]string{
		"WorkspacePath": workspacePath,
	}
	cmd := s.commands.GetRemoveWorkspace()
	if _, err := s.runTemplateCommand(ctx, cmd, vars); err != nil {
		return fmt.Errorf("remove workspace failed: %w", err)
	}
	return nil
}

func (s *SaplingBackend) PruneStale(ctx context.Context, repoBasePath string) error {
	return nil
}

func (s *SaplingBackend) IsBranchInUse(ctx context.Context, repoBasePath, branch string) (bool, error) {
	return false, nil
}

func (s *SaplingBackend) GetStatus(ctx context.Context, workspacePath string) (VCSStatus, error) {
	var status VCSStatus

	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath, "status")
	if err == nil {
		trimmed := strings.TrimSpace(string(output))
		status.Dirty = len(trimmed) > 0
		if trimmed != "" {
			status.FilesChanged = len(strings.Split(trimmed, "\n"))
		}
	}

	output, err = s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"log", "-r", ".", "-T", "{activebookmark}")
	if err == nil {
		status.CurrentBranch = strings.TrimSpace(string(output))
	}

	output, err = s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"log", "-r", "draft()", "-T", "x")
	if err == nil {
		status.AheadOfDefault = len(strings.TrimSpace(string(output)))
	}

	output, err = s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"diff", "--stat")
	if err == nil {
		status.LinesAdded, status.LinesRemoved = parseDiffStat(string(output))
	}

	return status, nil
}

func parseDiffStat(output string) (added, removed int) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	summary := lines[len(lines)-1]
	for _, part := range strings.Split(summary, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "insertion") {
			fields := strings.Fields(part)
			if len(fields) >= 1 {
				added, _ = strconv.Atoi(fields[0])
			}
		} else if strings.Contains(part, "deletion") {
			fields := strings.Fields(part)
			if len(fields) >= 1 {
				removed, _ = strconv.Atoi(fields[0])
			}
		}
	}
	return added, removed
}

func (s *SaplingBackend) GetChangedFiles(ctx context.Context, workspacePath string) ([]VCSChangedFile, error) {
	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath, "status")
	if err != nil {
		return nil, err
	}
	return parseSaplingStatus(string(output)), nil
}

func parseSaplingStatus(output string) []VCSChangedFile {
	var files []VCSChangedFile
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		code := string(line[0])
		path := strings.TrimSpace(line[2:])
		status := "modified"
		switch code {
		case "M":
			status = "modified"
		case "A":
			status = "added"
		case "R":
			status = "deleted"
		case "?":
			status = "untracked"
		case "!":
			status = "deleted"
		}
		files = append(files, VCSChangedFile{
			Path:   path,
			Status: status,
		})
	}
	return files
}

func (s *SaplingBackend) GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error) {
	return "main", nil
}

func (s *SaplingBackend) GetCurrentBranch(ctx context.Context, workspacePath string) (string, error) {
	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"log", "-r", ".", "-T", "{activebookmark}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *SaplingBackend) Fetch(ctx context.Context, path string) error {
	_, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, path, "pull")
	return err
}

func (s *SaplingBackend) EnsureQueryRepo(ctx context.Context, repoIdentifier, path string) error {
	return nil
}

func (s *SaplingBackend) FetchQueryRepo(ctx context.Context, path string) error {
	return nil
}

func (s *SaplingBackend) ListRecentBranches(ctx context.Context, path string, limit int) ([]RecentBranch, error) {
	return nil, nil
}

func (s *SaplingBackend) GetBranchLog(ctx context.Context, path, branch string, limit int) ([]string, error) {
	return nil, nil
}
