package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func (s *Server) dependencyReport() detect.DependencyReport {
	s.depReportMu.RLock()
	defer s.depReportMu.RUnlock()
	return s.depReport
}

// refreshDependencies re-runs native detection (agents come from the manager)
// behind a single-flight guard and updates the cache.
func (s *Server) refreshDependencies(ctx context.Context) detect.DependencyReport {
	s.depRefreshMu.Lock()
	defer s.depRefreshMu.Unlock()
	rep := detect.DetectDependencies(ctx, s.models.GetDetectedTools())
	s.depReportMu.Lock()
	s.depReport = rep
	s.depReportMu.Unlock()
	return rep
}

// handleDependencies returns the grouped dependency report. ?refresh=1 re-runs
// native detection (agents come from the manager) behind a single-flight guard.
func (s *Server) handleDependencies(w http.ResponseWriter, r *http.Request) {
	rep := s.dependencyReport()
	if r.URL.Query().Get("refresh") == "1" {
		rep = s.refreshDependencies(r.Context())
	}
	resp := buildDependenciesResponse(rep, runtime.GOOS)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "dependencies", "err", err)
	}
}

// buildDependenciesResponse maps the detect report to the wire contract,
// nesting deps under Groups and OS-filtering each dep's install methods.
func buildDependenciesResponse(rep detect.DependencyReport, goos string) contracts.DependenciesResponse {
	osTag := "linux"
	if goos == "darwin" {
		osTag = "macos"
	}
	byGroup := map[string][]contracts.Dependency{}
	for _, st := range rep.Statuses {
		methods := detect.InstallForOS(st.Install, goos, rep.PackageManagers)
		cms := make([]contracts.DependencyInstallMethod, len(methods))
		for i, m := range methods {
			cms[i] = contracts.DependencyInstallMethod{OS: m.OS, Label: m.Label, Command: m.Command, URL: m.URL, Requires: m.Requires}
		}
		byGroup[st.Group] = append(byGroup[st.Group], contracts.Dependency{
			ID: st.ID, DisplayName: st.DisplayName, Description: st.Description,
			Unlocks: st.Unlocks, DocsURL: st.DocsURL, Detected: st.Detected,
			Command: st.Command, Source: st.Source, Install: cms,
		})
	}
	var groups []contracts.DependencyGroup
	for _, g := range detect.Groups {
		deps := byGroup[g.ID]
		if len(deps) == 0 {
			continue
		}
		groups = append(groups, contracts.DependencyGroup{
			ID: g.ID, DisplayName: g.DisplayName, Description: g.Description, Dependencies: deps,
		})
	}
	return contracts.DependenciesResponse{OS: osTag, Groups: groups}
}
