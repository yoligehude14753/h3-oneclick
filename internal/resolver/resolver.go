package resolver

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"h3oneclick.local/launcher/internal/model"
)

type Resolver struct {
	Manifest model.Manifest
}

func New(m model.Manifest) *Resolver { return &Resolver{Manifest: m} }

func (r *Resolver) Resolve(hw model.HardwareSnapshot, instances []model.Instance, req model.BestConfigRequest) model.BestConfigDecision {
	preferred := req.Preference.PreferredSteps
	if preferred != 4 && preferred != 8 && preferred != 20 {
		preferred = req.Workload.PreferredSteps
	}
	if preferred != 4 && preferred != 8 && preferred != 20 {
		preferred = 4
	}
	policy := req.SourcePolicy
	if !policy.AllowSameFileMirror && !policy.AllowCompatibleAlternative {
		// Same-file mirrors are the safe default; callers can explicitly disable them.
		policy.AllowSameFileMirror = true
	}

	decision := model.BestConfigDecision{
		PreferredSteps: preferred,
		SourcePolicy:   policy,
		ComputedAt:     time.Now().UTC(),
	}
	type candidate struct {
		profile model.Profile
		score   float64
		reasons []string
	}
	var candidates []candidate
	profiles := r.Manifest.Profiles
	for _, p := range profiles {
		if !p.Auto && !(req.OverrideProfile == p.ID && policy.AllowCompatibleAlternative) {
			if p.ID != req.OverrideProfile {
				decision.Alternatives = append(decision.Alternatives, model.Alternative{ProfileID: p.ID, StackID: first(p.StackIDs), Status: "blocked", Reasons: []string{"manual_only"}, SourceTier: p.SourceTier})
				continue
			}
		}
		reasons, status := r.gates(p, hw, instances, req)
		alt := model.Alternative{ProfileID: p.ID, StackID: first(p.StackIDs), Status: status, Reasons: reasons, NewAssets: append([]string{}, p.Assets...), SourceTier: p.SourceTier}
		if p.ID != req.OverrideProfile {
			decision.Alternatives = append(decision.Alternatives, alt)
		}
		if status != "available" {
			continue
		}
		score := score(p, preferred, instances, req)
		candidates = append(candidates, candidate{profile: p, score: score, reasons: reasons})
	}

	if req.OverrideProfile != "" {
		for _, c := range candidates {
			if c.profile.ID == req.OverrideProfile {
				decision.UserOverride = req.OverrideProfile
				decision.RecommendedProfile = c.profile.ID
				decision.RecommendedStack = first(c.profile.StackIDs)
				decision.EffectiveProfile = c.profile.ID
				decision.ReasonCodes = append([]string{"user_override"}, c.reasons...)
				decision.FallbackChain = append([]string{}, c.profile.FallbackProfiles...)
				return decision
			}
		}
		decision.UserOverride = req.OverrideProfile
		decision.ReasonCodes = []string{"override_blocked"}
		decision.EffectiveProfile = ""
		return decision
	}

	if len(candidates) == 0 {
		decision.RecommendedProfile = "unsupported"
		decision.ReasonCodes = []string{"no_compatible_stack"}
		return decision
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	decision.RecommendedProfile = best.profile.ID
	decision.RecommendedStack = first(best.profile.StackIDs)
	decision.EffectiveProfile = best.profile.ID
	decision.ReasonCodes = append([]string{"stack_gates_pass"}, best.reasons...)
	decision.FallbackChain = append([]string{}, best.profile.FallbackProfiles...)
	return decision
}

func (r *Resolver) gates(p model.Profile, hw model.HardwareSnapshot, instances []model.Instance, req model.BestConfigRequest) ([]string, string) {
	var reasons []string
	var unknown bool
	osName := hw.OS
	if osName == "darwin" {
		osName = "macos"
	}
	if !contains(p.Platforms, osName) {
		return []string{"platform_mismatch"}, "blocked"
	}
	if !contains(p.Backends, hw.Backend) {
		return []string{"backend_mismatch"}, "blocked"
	}
	if p.Requires.RequiresAppleSilicon && !(osName == "macos" && hw.Arch == "arm64") {
		return []string{"apple_silicon_required"}, "blocked"
	}
	if p.SourceTier == "compatible_alternative" && !req.SourcePolicy.AllowCompatibleAlternative {
		return []string{"compatible_alternative_disabled"}, "blocked"
	}
	if p.ResearchOnly {
		return []string{"research_only"}, "blocked"
	}
	if p.Requires.MinFreeVRAMGiB != nil {
		available := maxFreeVRAM(hw)
		if available == 0 {
			available = maxTotalVRAM(hw)
		}
		if available == 0 {
			unknown = true
			reasons = append(reasons, "vram_unknown")
		} else if available < *p.Requires.MinFreeVRAMGiB {
			return []string{fmt.Sprintf("vram_below_stack_floor_%.0fgib", *p.Requires.MinFreeVRAMGiB)}, "blocked"
		} else {
			reasons = append(reasons, "vram_gate_pass")
		}
	}
	if p.Requires.MinRAMGiB != nil {
		if hw.RAMGiB == 0 {
			unknown = true
			reasons = append(reasons, "ram_unknown")
		} else if hw.RAMGiB < *p.Requires.MinRAMGiB {
			return []string{fmt.Sprintf("ram_below_stack_floor_%.0fgib", *p.Requires.MinRAMGiB)}, "blocked"
		} else {
			reasons = append(reasons, "ram_gate_pass")
		}
	}
	if p.Requires.ComputeCapabilityMin != nil {
		capability, err := strconv.ParseFloat(hw.ComputeCapability, 64)
		if err != nil {
			unknown = true
			reasons = append(reasons, "compute_capability_unknown")
		} else if capability < *p.Requires.ComputeCapabilityMin {
			return []string{"compute_capability_below_stack_floor"}, "blocked"
		}
	}
	if p.Requires.RequiresComfy {
		if len(instances) == 0 {
			reasons = append(reasons, "new_comfy_instance_required")
		} else {
			reasons = append(reasons, "comfy_instance_found")
		}
	}
	if p.RuntimeType == "comfyui" && p.QualityGate.Audio && !hasAsset(r.Manifest, p.Assets, "audio_vae") {
		return []string{"audio_vae_missing"}, "blocked"
	}
	if !p.QualityGate.Verified {
		reasons = append(reasons, "quality_gate_pending")
	}
	if unknown {
		return reasons, "unknown"
	}
	return reasons, "available"
}

func maxFreeVRAM(hw model.HardwareSnapshot) float64 {
	best := 0.0
	for _, gpu := range hw.GPUs {
		if gpu.VRAMFreeGiB > best {
			best = gpu.VRAMFreeGiB
		}
	}
	if best > 0 {
		return best
	}
	return hw.VRAMFreeGiB
}

func maxTotalVRAM(hw model.HardwareSnapshot) float64 {
	best := 0.0
	for _, gpu := range hw.GPUs {
		if gpu.VRAMGiB > best {
			best = gpu.VRAMGiB
		}
	}
	if best > 0 {
		return best
	}
	return hw.VRAMGiB
}

func score(p model.Profile, preferred int, instances []model.Instance, req model.BestConfigRequest) float64 {
	quality := 0.65
	if p.QualityGate.Verified {
		quality = 1
	}
	stability := 0.85
	if p.SourceTier == "official_base" {
		stability = 1
	}
	speed := 0.45
	if containsInt(p.Steps, 4) {
		speed = 1
	} else if containsInt(p.Steps, 8) {
		speed = 0.8
	} else if containsInt(p.Steps, 20) {
		speed = 0.55
	}
	reuse := 0.35
	if len(instances) > 0 {
		reuse = 1
	}
	preference := 0.25
	if containsInt(p.Steps, preferred) {
		preference = 1
	}
	if req.Workload.Audio && !p.QualityGate.Audio {
		quality *= 0.5
	}
	// Step preference only affects the soft score; hardware gates were already evaluated.
	return 0.35*quality + 0.25*stability + 0.20*speed + 0.10*reuse + 0.10*preference
}

func hasAsset(m model.Manifest, ids []string, kind string) bool {
	for _, id := range ids {
		for _, a := range m.Assets {
			name := strings.ToLower(a.ID + " " + a.Name)
			if a.ID == id && (a.Kind == kind || (kind == "audio_vae" && a.Kind == "vae" && strings.Contains(name, "audio"))) {
				return true
			}
		}
	}
	return false
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
