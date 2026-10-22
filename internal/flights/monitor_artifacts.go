package flights

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// artifactLine matches the planner's report lines. The planner prompt asks
// for exactly `Spec: <absolute path>` and `Plan: <absolute path>`; anything
// else on the line is part of the reported path and fails verification.
var artifactLine = regexp.MustCompile(`(?m)^(Spec|Plan):[ \t]*(.*?)[ \t]*$`)

// parseArtifactReport returns the last `Spec:` and `Plan:` values in text and
// whether either line appeared.
func parseArtifactReport(text string) (spec, plan string, found bool) {
	for _, m := range artifactLine.FindAllStringSubmatch(text, -1) {
		found = true
		if m[1] == "Spec" {
			spec = m[2]
		} else {
			plan = m[2]
		}
	}
	return spec, plan, found
}

// verifyArtifacts checks a reported spec and plan against the placement
// contract: both reported as absolute paths to distinct, nonempty regular
// files inside the workspace that git does not ignore (the skills commit
// them). It returns every violation found, joined, or "" when both pass.
func verifyArtifacts(ctx context.Context, ws *WorkspaceAssignment, spec, plan string) string {
	var problems []string
	for _, a := range []struct{ name, path string }{{"spec", spec}, {"plan", plan}} {
		if err := verifyArtifact(ctx, ws, a.name, a.path); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if spec != "" && spec == plan {
		problems = append(problems, "spec and plan report the same file")
	}
	return strings.Join(problems, "; ")
}

func verifyArtifact(ctx context.Context, ws *WorkspaceAssignment, name, path string) error {
	if path == "" {
		return fmt.Errorf("no %s reported", name)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s %q is not an absolute path", name, path)
	}
	if err := checkArtifact(path); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	root, rel, err := insideWorkspace(ws, name, path)
	if err != nil {
		return err
	}
	if ws.VCS != "git" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-q", "--", rel)
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return fmt.Errorf("%s %s is ignored by git, so it cannot be committed", name, path)
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return nil
	default:
		return fmt.Errorf("%s: git check-ignore %s: %w", name, path, err)
	}
}

// insideWorkspace resolves path against the workspace and returns the
// resolved workspace root and path's location relative to it. It fails when
// the path leaves the workspace. Workspaces are commonly reached through
// symlinked roots, so it compares resolved paths.
func insideWorkspace(ws *WorkspaceAssignment, name, path string) (root, rel string, err error) {
	if ws == nil || ws.Path == "" {
		return "", "", fmt.Errorf("%s: flight has no workspace to check against", name)
	}
	root, err = filepath.EvalSymlinks(ws.Path)
	if err != nil {
		return "", "", fmt.Errorf("%s: resolve workspace %s: %w", name, ws.Path, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("%s: resolve %s: %w", name, path, err)
	}
	rel, err = filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%s %s is outside the workspace %s", name, path, ws.Path)
	}
	return root, rel, nil
}

// recordArtifactReport parses a completed planner turn for its spec/plan
// report, verifies it, and retains the result on the turn. A verified report
// becomes the flight's SpecPath and PlanPath; a violating report leaves the
// last verified paths in place. A turn with no report lines records nothing.
func (m *Manager) recordArtifactReport(ctx context.Context, flightID string, t MonitoredTurn) {
	spec, plan, found := parseArtifactReport(t.Turn.Text)
	if !found {
		return
	}
	f, ok := m.Get(flightID)
	if !ok {
		return
	}
	report := ArtifactReport{SpecPath: spec, PlanPath: plan, Error: verifyArtifacts(ctx, f.Workspace, spec, plan)}
	if ctx.Err() != nil {
		return
	}
	err := m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			return fmt.Errorf("monitor: flight %s has no monitor", f.ID)
		}
		for i := range f.Monitor.Turns {
			if f.Monitor.Turns[i].ID != t.ID {
				continue
			}
			r := report
			f.Monitor.Turns[i].Artifacts = &r
			f.Monitor.Turns[i].UpdatedAt = m.deps.Now()
			if report.Error == "" {
				f.SpecPath, f.PlanPath = spec, plan
			}
			return nil
		}
		return fmt.Errorf("monitor: flight %s has no turn %s", f.ID, t.ID)
	})
	if err != nil {
		m.logger.Warn("monitor: retain artifact report failed", "err", err)
	}
}
