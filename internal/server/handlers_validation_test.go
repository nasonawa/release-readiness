package server

import (
	"net/http"
	"testing"

	"github.com/quay/release-readiness/internal/model"
)

func TestMissingValidationRelease(t *testing.T) {
	var response map[string]any
	getJSON(t, setupTestServer(t), "/api/v1/releases/quay-v99.0.0/validation", http.StatusNotFound, &response)
}

func TestValidationRequiresSourceSuccess(t *testing.T) {
	s := setupTestServer(t)
	v := s.validation("quay-3-18", &model.ReleaseCandidate{Verdict: model.Verdict{State: "ready"}}, nil)
	if v.Ready || v.SourceFresh || v.PlanKnown {
		t.Fatalf("empty instance looks ready: %+v", v)
	}
}
