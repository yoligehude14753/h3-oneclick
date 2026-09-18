package resolver

import (
	"testing"

	"h3oneclick.local/launcher/internal/manifest"
	"h3oneclick.local/launcher/internal/model"
)

func TestResolverSeparatesStepPreferenceFromHardware(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	r := New(m)
	hw := model.HardwareSnapshot{OS: "windows", Arch: "amd64", Backend: "cuda", VRAMGiB: 24, VRAMFreeGiB: 24, RAMGiB: 64}
	decision := r.Resolve(hw, nil, model.BestConfigRequest{
		Hardware:     &hw,
		Workload:     model.WorkloadRequest{Task: "t2v", Width: 768, Height: 432, Seconds: 5, Audio: true, PreferredSteps: 4},
		Preference:   model.Preference{PreferredSteps: 4, QualityFloor: "baseline"},
		SourcePolicy: model.SourcePolicy{AllowSameFileMirror: true},
	})
	if decision.EffectiveProfile != "turbo-4-768" {
		t.Fatalf("expected turbo-4-768 for a compatible 24GB stack, got %q (%v)", decision.EffectiveProfile, decision.ReasonCodes)
	}
}

func TestResolverMacUsesExternalRuntime(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	r := New(m)
	hw := model.HardwareSnapshot{OS: "darwin", Arch: "arm64", Backend: "mps", UnifiedMemoryGiB: 24, UnifiedMemoryFreeGiB: 20, RAMGiB: 24}
	decision := r.Resolve(hw, nil, model.BestConfigRequest{
		Hardware:     &hw,
		Workload:     model.WorkloadRequest{Task: "t2v", Width: 512, Height: 512, Seconds: 1, Audio: true, PreferredSteps: 4},
		Preference:   model.Preference{PreferredSteps: 4},
		SourcePolicy: model.SourcePolicy{AllowSameFileMirror: true},
	})
	if decision.EffectiveProfile != "macos-metal-h3c" {
		t.Fatalf("expected macos-metal-h3c, got %q", decision.EffectiveProfile)
	}
}

func TestResolverDoesNotSilentlyEnableAlternative(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	r := New(m)
	hw := model.HardwareSnapshot{OS: "windows", Arch: "amd64", Backend: "cuda", VRAMGiB: 24, VRAMFreeGiB: 24, RAMGiB: 64}
	decision := r.Resolve(hw, nil, model.BestConfigRequest{
		Hardware:        &hw,
		OverrideProfile: "runninghub-int8-cn",
		Workload:        model.WorkloadRequest{Task: "t2v", Width: 832, Height: 480, Seconds: 5, Audio: true, PreferredSteps: 20},
		Preference:      model.Preference{PreferredSteps: 20},
		SourcePolicy:    model.SourcePolicy{AllowSameFileMirror: true, AllowCompatibleAlternative: false},
	})
	if decision.EffectiveProfile != "" || decision.ReasonCodes[0] != "override_blocked" {
		t.Fatalf("alternative profile was silently enabled: %#v", decision)
	}
}

func TestResolverUsesPerGPUFreeMemoryNotAggregate(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	hw := model.HardwareSnapshot{
		OS: "linux", Arch: "amd64", Backend: "cuda", RAMGiB: 128,
		VRAMGiB: 64, VRAMFreeGiB: 30,
		GPUs: []model.GPUInfo{{Name: "gpu0", Backend: "cuda", VRAMGiB: 32, VRAMFreeGiB: 15}, {Name: "gpu1", Backend: "cuda", VRAMGiB: 32, VRAMFreeGiB: 15}},
	}
	decision := New(m).Resolve(hw, nil, model.BestConfigRequest{Workload: model.WorkloadRequest{Task: "t2v", Audio: true, PreferredSteps: 4}, Preference: model.Preference{PreferredSteps: 4}, SourcePolicy: model.SourcePolicy{AllowSameFileMirror: true}})
	if decision.EffectiveProfile != "" || decision.RecommendedProfile != "unsupported" {
		t.Fatalf("aggregate free VRAM incorrectly passed a single-GPU stack: %#v", decision)
	}
}
