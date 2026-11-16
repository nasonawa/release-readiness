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

func TestInventoryConfigurationLabels(t *testing.T) {
	for _, tc := range []struct{ cloud, target, name, wantCloud, wantStorage string }{
		{"aws", "aws-s3-nightly", "install", "AWS", "S3"},
		{"aws", "aws-metal-s3-nightly", "install", "AWS", "S3"},
		{"aws", "aws-odf-nightly", "install", "AWS", "ODF"},
		{"aws", "aws-s3-rds-nightly", "install", "AWS", "S3"},
		{"gcp", "gcp-gcs-nightly", "install", "GCP", "GCS"},
		{"azure4", "azure-blob-postgres-nightly", "install", "Azure", "Blob"},
		{"libvirt-s390x-vpn", "libvirt-s3-nightly", "install", "Libvirt", "S3"},
		{"aws", "from-318", "periodic-ci-quay-quay-redhat-5.0-aws-ocp50-s3-upgrade-from-318", "AWS", "S3"},
		{"aws", "e2e", "chainsaw", "AWS", ""},
		{"custom", "custom-newstorage", "install", "custom", ""},
	} {
		t.Run(tc.target, func(t *testing.T) {
			r := NewPlanRegistry()
			r.Update([]prow.JobDefinition{{Name: tc.name, Application: "quay-5-0", Cloud: tc.cloud, Target: tc.target}}, "rev")
			row := r.Plan("quay-5-0").Jobs[0]
			if row.Cloud != tc.wantCloud || row.Storage != tc.wantStorage {
				t.Fatalf("configuration = %q / %q, want %q / %q", row.Cloud, row.Storage, tc.wantCloud, tc.wantStorage)
			}
		})
	}
}
