package release

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/prow"
)

// PlanRegistry publishes immutable complete inventories to concurrent API reads.
type PlanRegistry struct {
	base    map[string]ValidationPlan
	current atomic.Pointer[map[string]ValidationPlan]
}

func NewPlanRegistry(base map[string]ValidationPlan) *PlanRegistry {
	r := &PlanRegistry{base: MergeValidationInventory(base, nil, "")}
	r.Update(nil, "")
	return r
}

func (r *PlanRegistry) Update(jobs []prow.JobDefinition, revision string) {
	plans := MergeValidationInventory(r.base, jobs, revision)
	r.current.Store(&plans)
}

func (r *PlanRegistry) Plan(app string) ValidationPlan { return (*r.current.Load())[app] }

// MergeValidationInventory preserves explicit future requirements and fills in
// authoritative periodic names/targets. Removed configured jobs remain missing.
func MergeValidationInventory(base map[string]ValidationPlan, jobs []prow.JobDefinition, revision string) map[string]ValidationPlan {
	plans := make(map[string]ValidationPlan, len(base))
	for app, p := range base {
		p.Jobs = append([]model.ValidationRow(nil), p.Jobs...)
		if revision != "" {
			for i := range p.Jobs {
				p.Jobs[i].Unconfigured = true
			}
		}
		plans[app] = p
	}
	for _, j := range jobs {
		p := plans[j.Application]
		idx := -1
		for i, row := range p.Jobs {
			if row.Job == j.Name {
				idx = i
				break
			}
		}
		if idx < 0 {
			p.Jobs = append(p.Jobs, model.ValidationRow{Job: j.Name})
			idx = len(p.Jobs) - 1
		}
		row := &p.Jobs[idx]
		row.OCP = cmp.Or(j.OCP, row.OCP)
		row.Cloud = cmp.Or(j.Cloud, row.Cloud)
		row.SourceURL = j.SourceURL
		row.Removed = j.Removed
		row.Unconfigured = false
		plans[j.Application] = p
	}
	if revision != "" {
		for app, p := range plans {
			p.Revision = "periodics:" + revision + ";plan:" + p.Revision
			plans[app] = p
		}
	}
	return plans
}

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
	// Index candidate-matched runs once, rather than scanning every run for
	// every required matrix row.
	completedByJob, runningByJob := map[string]*prow.Run{}, map[string]*prow.Run{}
	for i := range runs {
		r := &runs[i]
		if !exact[r.JobName][r.ProwURL] {
			continue
		}
		if r.CompletedAt != nil && (r.State == "success" || r.State == "failure" || r.State == "error" || r.State == "aborted") {
			old := completedByJob[r.JobName]
			if old == nil || r.CompletedAt.After(*old.CompletedAt) || (r.CompletedAt.Equal(*old.CompletedAt) && r.State != "success") {
				completedByJob[r.JobName] = r
			}
		} else if r.State == "pending" || r.State == "running" || r.State == "triggered" {
			runningByJob[r.JobName] = r
		}
	}
	for _, spec := range plan.Jobs {
		row := spec
		row.State, row.RunURL = "missing", ""
		completed, running := completedByJob[spec.Job], runningByJob[spec.Job]
		if spec.Removed {
			completed, running = nil, nil
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
