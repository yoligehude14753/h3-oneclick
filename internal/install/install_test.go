package install

import (
	"os"
	"path/filepath"
	"testing"

	"h3oneclick.local/launcher/internal/manifest"
	"h3oneclick.local/launcher/internal/model"
	"h3oneclick.local/launcher/internal/source"
)

func TestBuildPlanRequiresRuntimeBootstrapForEmptyComfyUI(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	planner := NewPlanner(m, source.NewRegistry(m))
	hw := model.HardwareSnapshot{OS: "windows", Arch: "amd64", Backend: "cuda", VRAMGiB: 24, VRAMFreeGiB: 24, RAMGiB: 64, DiskFreeGiB: 200}
	decision := model.BestConfigDecision{EffectiveProfile: "native-20", RecommendedProfile: "native-20", PreferredSteps: 20, SourcePolicy: model.SourcePolicy{AllowSameFileMirror: true}}
	plan, err := planner.BuildPlan(model.PlanRequest{Hardware: &hw, Workload: model.WorkloadRequest{Task: "t2v", Width: 768, Height: 432, Seconds: 5, Audio: true, PreferredSteps: 20}, SourcePolicy: model.SourcePolicy{AllowSameFileMirror: true}, TargetDir: t.TempDir(), BootstrapRuntime: false}, decision, model.ScanResult{Hardware: hw})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked || len(plan.BlockReasons) == 0 {
		t.Fatalf("expected runtime bootstrap gate, got %#v", plan)
	}
}

func TestBuildPlanEnablesRuntimeBootstrap(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	planner := NewPlanner(m, source.NewRegistry(m))
	hw := model.HardwareSnapshot{OS: "windows", Arch: "amd64", Backend: "cuda", VRAMGiB: 24, VRAMFreeGiB: 24, RAMGiB: 64, DiskFreeGiB: 200}
	root := t.TempDir()
	plan, err := planner.BuildPlan(model.PlanRequest{Hardware: &hw, Workload: model.WorkloadRequest{Task: "t2v", Width: 768, Height: 432, Seconds: 5, Audio: true, PreferredSteps: 20}, SourcePolicy: model.SourcePolicy{AllowSameFileMirror: true}, TargetDir: root, BootstrapRuntime: true}, model.BestConfigDecision{EffectiveProfile: "native-20", PreferredSteps: 20}, model.ScanResult{Hardware: hw})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked || !plan.RuntimeBootstrap || plan.RuntimeRoot == "" {
		t.Fatalf("expected runtime bootstrap plan, got %#v", plan)
	}
}

func TestInstallTemplateAdapter(t *testing.T) {
	root := t.TempDir()
	if err := installTemplateAdapter(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "custom_nodes", "H3OneClickAdapter", "__init__.py"),
		filepath.Join(root, "custom_nodes", "H3OneClickAdapter", "web", "h3-oneclick.js"),
	} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("adapter file missing: %s (%v)", path, err)
		}
	}
}
