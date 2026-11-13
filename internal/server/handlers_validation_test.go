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

func TestJiraReleaseDoesNotConfirmCustomerPublication(t *testing.T) {
	s := setupTestServer(t)
	const name = "quay-v3.18.1"
	if err := s.db.UpsertReleaseVersion(t.Context(), &model.ReleaseVersion{
		Name: name, KonfluxApplication: "quay-3-18", Released: true,
	}); err != nil {
		t.Fatal(err)
	}
	var rows []model.ReleaseOverview
	getJSON(t, s, "/api/v1/releases/overview?portfolio=1", http.StatusOK, &rows)
	if len(rows) != 1 || !rows[0].Shipped || rows[0].Published || rows[0].Portfolio == nil || rows[0].Portfolio.Stage == "available" {
		t.Fatalf("JIRA-only release advertised as published: %+v", rows)
	}
	var detail struct {
		Published bool `json:"published"`
	}
	getJSON(t, s, "/api/v1/releases/"+name, http.StatusOK, &detail)
	if detail.Published {
		t.Fatal("detail marks JIRA-only release published")
	}
}
