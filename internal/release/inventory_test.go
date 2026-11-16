package release

import (
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
	"testing"
)

func TestInventoryPreservesPlannedAndRemovedRequirements(t *testing.T) {
	base := map[string]ValidationPlan{"quay-3-18": {Revision: "plan", Jobs: []model.ValidationRow{{Job: "future"}, {Job: "old"}}}}
	jobs := []prow.JobDefinition{{Name: "new", Application: "quay-3-18"}, {Name: "old", Application: "quay-3-18", Removed: true}}
	plans := MergeValidationInventory(base, jobs, "config")
	if len(plans["quay-3-18"].Jobs) != 3 || len(base["quay-3-18"].Jobs) != 2 {
		t.Fatal("lost planned requirement or mutated base")
	}
	for _, row := range plans["quay-3-18"].Jobs {
		if row.Job == "old" && !row.Removed {
			t.Fatal("removed job not retained")
		}
	}
	v := EvaluateValidation(plans["quay-3-18"], &model.ReleaseCandidate{Verdict: model.Verdict{State: "ready"}}, nil, true)
	if v.Ready || v.Total != 3 || v.Missing != 3 {
		t.Fatalf("unexpected coverage %+v", v)
	}
}
