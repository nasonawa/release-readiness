package release

import (
	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
	"testing"
)

func TestInventoryOnlyCurrentPeriodicsAndKnownEmpty(t *testing.T) {
	r := NewPlanRegistry()
	c := &model.ReleaseCandidate{Verdict: model.Verdict{State: "ready"}}
	if EvaluateValidation(r.Plan("quay-3-18"), c, nil, true).PlanKnown {
		t.Fatal("unloaded inventory looks known")
	}
	r.Update([]prow.JobDefinition{{Name: "new", Application: "quay-3-18"}, {Name: "old", Application: "quay-3-17", Removed: true}}, "rev")
	v := EvaluateValidation(r.Plan("quay-3-18"), c, nil, true)
	if !v.PlanKnown || v.Total != 1 || v.Missing != 1 || v.Ready {
		t.Fatalf("configured job without runs: %+v", v)
	}
	for _, app := range []string{"quay-3-17", "omr-2-0", "quay-99-0"} {
		v = EvaluateValidation(r.Plan(app), c, nil, true)
		if !v.PlanKnown || v.Total != 0 || v.Ready {
			t.Fatalf("no CI configured: %+v", v)
		}
	}
}
