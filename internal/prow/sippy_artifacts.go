package prow

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

func (s *Syncer) fetchIndexedRun(ctx context.Context, job JobDefinition, row IndexedRun) (*Run, error) {
	run := &Run{JobName: row.Job, BuildID: row.ID, Application: job.Application, ProwURL: row.URL, StartedAt: &row.Timestamp, State: "unknown", ArtifactState: ArtifactMissing, FetchedAt: time.Now().UTC()}
	// Unknown result codes cannot become success via succeeded=true.
	if !slices.Contains([]string{"S", "F", "I", "U", "N", "n", "A", "f", "R"}, row.Result) {
		return run, nil
	}
	if row.Result == "R" {
		run.State = "running"
		return run, nil
	}
	dir := job.Job().Prefix + row.ID + "/"
	finData, err := s.client.object(ctx, dir+"finished.json")
	if err != nil {
		return nil, err
	}
	if finData == nil {
		return run, nil // no fabricated completion time or terminal success
	}
	var fin finished
	if err := json.Unmarshal(finData, &fin); err != nil {
		return nil, err
	}
	if fin.Timestamp <= 0 || !slices.Contains([]string{"SUCCESS", "FAILURE", "ERROR", "ABORTED"}, fin.Result) {
		return nil, fmt.Errorf("invalid completion record")
	}
	completed := time.Unix(fin.Timestamp, 0).UTC()
	run.State, run.CompletedAt = strings.ToLower(fin.Result), &completed
	target := job.Target
	var artifact []byte
	if target != "" {
		artifact, err = s.client.object(ctx, dir+"artifacts/"+target+"/quay-gather/artifacts/tested-images.json")
		if err != nil {
			return nil, err
		}
	}
	// A current config may not describe an older run. Only fall back to
	// prowjob.json when the configured target cannot locate its artifact.
	if target == "" || artifact == nil {
		pj, err := s.client.object(ctx, dir+"prowjob.json")
		if err != nil {
			return nil, err
		}
		if pj == nil {
			return run, nil
		}
		_, actual, err := parseRun(job.Job(), row.ID, pj, finData)
		if err != nil {
			return nil, err
		}
		if actual != "" && !artifactTarget.MatchString(actual) {
			return nil, fmt.Errorf("invalid artifact target")
		}
		if actual != target && actual != "" {
			target = actual
			artifact, err = s.client.object(ctx, dir+"artifacts/"+target+"/quay-gather/artifacts/tested-images.json")
			if err != nil {
				return nil, err
			}
		}
	}
	run.ArtifactState = ArtifactNone
	if target == "" {
		return run, nil
	}
	if artifact != nil {
		if run.applyArtifact(artifact) && run.State != "success" {
			run.InstallFailed, err = s.installFailed(ctx, job.Job(), row.ID, target)
		}
		return run, err
	}
	csv, err := s.client.object(ctx, dir+"artifacts/"+target+"/quay-gather/artifacts/olm-csvs.yaml")
	if err != nil {
		return nil, err
	}
	if csv != nil {
		run.applyOLMCSVs(csv)
	}
	return run, nil
}
