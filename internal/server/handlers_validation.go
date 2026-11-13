package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
	"github.com/quay/release-readiness/internal/release"
)

func (s *Server) validation(app string, candidate *model.ReleaseCandidate, runs []prow.Run) model.Validation {
	fresh := true
	// No successful source pass is not evidence of freshness.
	for _, name := range []string{"prow", "konflux", "art-history", "fbc-render"} {
		found := false
		for _, source := range s.syncStatus.Sources() {
			if source.Source == name && source.LastSuccess != nil {
				found = true
			}
		}
		if !found {
			fresh = false
		}
	}
	for _, problem := range s.syncStatus.Problems(time.Now()) {
		switch problem.Source {
		case "prow", "konflux", "art-history", "fbc-render":
			fresh = false
		}
	}
	return release.EvaluateValidation(s.ValidationPlans[app], candidate, runs, fresh)
}

func (s *Server) handleValidation(w http.ResponseWriter, r *http.Request) {
	rel, err := s.db.GetReleaseVersion(r.Context(), r.PathValue("version"))
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("release not found"))
		return
	}
	candidate, runs, err := s.selector.ReleaseCandidate(r.Context(), rel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.validation(rel.KonfluxApplication, candidate, runs))
}
