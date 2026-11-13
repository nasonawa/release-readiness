package release

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
)

// ValidationPlan comes from reviewed job configurations, never inferred from
// the runs that happened to execute. Applications share a minor-stream plan.
type ValidationPlan struct {
	Revision string                `json:"revision"`
	Jobs     []model.ValidationRow `json:"jobs"`
}

func LoadValidationPlans(path string) (map[string]ValidationPlan, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plans map[string]ValidationPlan
	if err := json.Unmarshal(b, &plans); err != nil {
		return nil, err
	}
	for app, plan := range plans {
		if app == "" || plan.Revision == "" || len(plan.Jobs) == 0 {
			return nil, fmt.Errorf("validation plan %q needs a revision and required jobs", app)
		}
		seen := map[string]bool{}
		for _, row := range plan.Jobs {
			if strings.TrimSpace(row.Job) == "" || seen[row.Job] {
				return nil, fmt.Errorf("validation plan %q has an empty or duplicate job %q", app, row.Job)
			}
			seen[row.Job] = true
		}
	}
	return plans, nil
}

// EvaluateValidation accepts exact selected-FBC evidence only. Equivalent
// bundles and unverified image identities remain available in CI drill-downs,
// but cannot satisfy a shipment gate. Unsupported identities remain missing.
func EvaluateValidation(plan ValidationPlan, candidate *model.ReleaseCandidate, runs []prow.Run, fresh bool) model.Validation {
	v := model.Validation{PlanKnown: len(plan.Jobs) > 0, Revision: plan.Revision, SourceFresh: fresh, Total: len(plan.Jobs)}
	exact := map[string]map[string]bool{}
	if candidate.FBCCandidate != nil {
		for _, op := range candidate.FBCCandidate.Operators {
			for _, member := range op.Members {
				if member.CI == nil {
					continue
				}
				for _, job := range member.CI.Exact {
					if exact[job.JobName] == nil {
						exact[job.JobName] = map[string]bool{}
					}
					for _, run := range job.Runs {
						if run.ProwURL != "" {
							exact[job.JobName][run.ProwURL] = true
						}
					}
				}
			}
		}
	}
	for _, spec := range plan.Jobs {
		row := spec
		row.State, row.RunURL = "missing", ""
		var completed, running *prow.Run
		for i := range runs {
			r := &runs[i]
			if r.JobName != spec.Job || !exact[r.JobName][r.ProwURL] {
				continue
			}
			if r.CompletedAt != nil && (r.State == "success" || r.State == "failure" || r.State == "error" || r.State == "aborted") {
				if completed == nil || r.CompletedAt.After(*completed.CompletedAt) || (r.CompletedAt.Equal(*completed.CompletedAt) && r.State != "success") {
					completed = r
				}
			} else if r.State == "pending" || r.State == "running" || r.State == "triggered" {
				running = r
			}
		}
		if completed != nil {
			row.RunURL = completed.ProwURL
			if completed.State == "success" && !completed.InstallFailed {
				row.State = "passed"
			} else {
				row.State = "failed"
			}
		} else if running != nil {
			row.State, row.RunURL = "running", running.ProwURL
		}
		switch row.State {
		case "passed":
			v.Passed++
		case "failed":
			v.Failed++
		case "running":
			v.Running++
		default:
			v.Missing++
		}
		v.Rows = append(v.Rows, row)
	}
	v.Ready = v.PlanKnown && v.Passed == v.Total && fresh && candidate.Verdict.State == "ready"
	return v
}
