package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"h3oneclick.local/launcher/internal/model"
	"h3oneclick.local/launcher/internal/source"
)

type Planner struct {
	Manifest model.Manifest
	Registry *source.Registry
}

func NewPlanner(m model.Manifest, registry *source.Registry) *Planner {
	return &Planner{Manifest: m, Registry: registry}
}

func (p *Planner) BuildPlan(req model.PlanRequest, decision model.BestConfigDecision, scan model.ScanResult) (model.InstallPlan, error) {
	profileID := req.ProfileID
	if profileID == "" {
		profileID = decision.EffectiveProfile
	}
	profile, ok := findProfile(p.Manifest, profileID)
	if !ok {
		return model.InstallPlan{}, fmt.Errorf("profile %q not found", profileID)
	}
	plan := model.InstallPlan{
		Platform:         scan.Hardware.OS,
		Backend:          scan.Hardware.Backend,
		ProfileID:        profile.ID,
		StackID:          first(profile.StackIDs),
		PreferredSteps:   decision.PreferredSteps,
		SourcePolicy:     req.SourcePolicy,
		AvailableFreeGiB: scan.Hardware.DiskFreeGiB,
	}
	if plan.SourcePolicy == (model.SourcePolicy{}) {
		plan.SourcePolicy.AllowSameFileMirror = true
	}
	instance, found := findInstance(scan.Instances, req.InstanceID)
	if found {
		plan.InstanceID = instance.ID
		plan.InstanceMode = "reuse-existing"
		plan.Port = instance.Port
	} else {
		plan.InstanceMode = "isolated-fallback"
		if profile.RuntimeType == "comfyui" {
			plan.RuntimeBootstrap = req.BootstrapRuntime
			plan.RuntimeSource = comfySourceURL
			if !req.BootstrapRuntime {
				plan.Blocked = true
				plan.BlockReasons = append(plan.BlockReasons, "runtime_missing_bootstrap_disabled")
			}
		}
	}
	root := req.TargetDir
	if root == "" {
		if found {
			root = instance.Path
		} else {
			root = filepath.Join(".", "h3-oneclick-runtime", profile.ID)
		}
	}
	assetRoot := root
	if plan.RuntimeBootstrap {
		assetRoot = filepath.Join(root, "ComfyUI")
		plan.RuntimeRoot = assetRoot
	}
	if profile.RuntimeType == "comfyui" {
		plan.ComfyRoot = assetRoot
		plan.TemplateAdapter = filepath.Join(assetRoot, "custom_nodes", "H3OneClickAdapter")
	}
	for _, assetID := range profile.Assets {
		asset, ok := findAsset(p.Manifest, assetID)
		if !ok {
			plan.Blocked = true
			plan.BlockReasons = append(plan.BlockReasons, "asset_missing:"+assetID)
			continue
		}
		sources, err := p.Registry.Sources(asset.ID, plan.SourcePolicy)
		if err != nil {
			plan.Blocked = true
			plan.BlockReasons = append(plan.BlockReasons, err.Error())
			continue
		}
		primary := sources[0]
		var mirrors []model.Source
		for _, candidate := range sources[1:] {
			if candidate.Role == "same_file_mirror" {
				mirrors = append(mirrors, candidate)
			}
		}
		target := filepath.Join(assetRoot, filepath.FromSlash(asset.TargetPath))
		if !withinRoot(assetRoot, target) {
			plan.Blocked = true
			plan.BlockReasons = append(plan.BlockReasons, "unsafe_target_path:"+asset.ID)
			continue
		}
		reuse := false
		if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && asset.SizeBytes > 0 && info.Size() == asset.SizeBytes {
			reuse = true
		}
		plan.Assets = append(plan.Assets, model.AssetPlan{
			AssetID: asset.ID, TargetPath: target, SizeBytes: asset.SizeBytes,
			Reuse: reuse, Primary: primary, Mirrors: mirrors,
		})
		if !reuse {
			plan.RequiredFreeGiB += float64(asset.SizeBytes) / 1024 / 1024 / 1024
		}
	}
	plan.RequiredFreeGiB = plan.RequiredFreeGiB*1.20 + 8
	if plan.AvailableFreeGiB > 0 && plan.RequiredFreeGiB > plan.AvailableFreeGiB {
		plan.Blocked = true
		plan.BlockReasons = append(plan.BlockReasons, "disk_space_below_plan")
	}
	if profile.RuntimeType == "external" {
		plan.Warnings = append(plan.Warnings, "该 profile 是非 ComfyUI runtime；不会自动声称已打开 ComfyUI 模板")
	}
	if plan.RuntimeBootstrap {
		plan.Warnings = append(plan.Warnings, "将从官方 ComfyUI Git 仓库创建独立运行时并安装 requirements.txt；首次安装可能较慢")
	}
	if plan.ComfyRoot != "" {
		plan.Warnings = append(plan.Warnings, "将安装 H3OneClickAdapter；新运行时会自动加载，已运行实例需要重启后才能前端自动选中模板")
	}
	if len(plan.Assets) == 0 && profile.RuntimeType == "comfyui" {
		plan.Warnings = append(plan.Warnings, "当前 profile 没有可下载资产；请确认已有 runtime 或先执行运行时引导")
	}
	return plan, nil
}

type Manager struct {
	Planner  *Planner
	StateDir string
	Registry *source.Registry
	mu       sync.RWMutex
	jobs     map[string]*model.Job
}

func NewManager(planner *Planner, registry *source.Registry, stateDir string) *Manager {
	return &Manager{Planner: planner, Registry: registry, StateDir: stateDir, jobs: make(map[string]*model.Job)}
}

func (m *Manager) Start(ctx context.Context, plan model.InstallPlan, dryRun bool) *model.Job {
	job := &model.Job{ID: fmt.Sprintf("job-%d", time.Now().UnixNano()), State: "SOURCE_PROBING", Plan: plan, Progress: map[string]any{"dry_run": dryRun}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	go m.run(context.WithoutCancel(ctx), job, dryRun)
	return job
}

func (m *Manager) Get(id string) (*model.Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	copy := *job
	copy.Attempts = append([]model.SourceAttempt(nil), job.Attempts...)
	return &copy, true
}

func (m *Manager) run(ctx context.Context, job *model.Job, dryRun bool) {
	if job.Plan.Blocked {
		m.update(job, "BLOCKED", "install plan blocked")
		return
	}
	if job.Plan.RuntimeBootstrap {
		m.update(job, "BOOTSTRAPPING_RUNTIME", job.Plan.RuntimeSource)
		if dryRun {
			m.setProgress(job, "runtime_bootstrap", "dry_run")
		} else if err := bootstrapComfy(ctx, job.Plan.RuntimeRoot); err != nil {
			m.update(job, "PARTIAL", err.Error())
			return
		}
	}
	if job.Plan.ComfyRoot != "" {
		m.update(job, "INSTALLING_TEMPLATE_ADAPTER", job.Plan.TemplateAdapter)
		if dryRun {
			m.setProgress(job, "template_adapter", "dry_run")
		} else if err := installTemplateAdapter(job.Plan.ComfyRoot); err != nil {
			m.update(job, "PARTIAL", err.Error())
			return
		}
	}
	for i, asset := range job.Plan.Assets {
		m.updateProgress(job, i, len(job.Plan.Assets), asset.AssetID)
		if asset.Reuse {
			continue
		}
		if dryRun {
			probe := m.Registry.Probe(ctx, asset.AssetID, job.Plan.SourcePolicy)
			m.appendAttempts(job, probe.Attempts...)
			if probe.SelectedSourceRole == "" {
				m.update(job, "SOURCE_BLOCKED", "no source passed probe for "+asset.AssetID)
				return
			}
			continue
		}
		m.update(job, "DOWNLOADING", asset.AssetID)
		attempts, _, err := m.Registry.DownloadAsset(ctx, asset.AssetID, asset.TargetPath, job.Plan.SourcePolicy)
		m.appendAttempts(job, attempts...)
		if err != nil {
			m.update(job, "SOURCE_BLOCKED", err.Error())
			return
		}
	}
	m.update(job, "INSTALLING", "writing receipt")
	status := "READY_FOR_BASELINE"
	if job.Plan.ProfileID == "macos-metal-h3c" {
		status = "NON_COMFYUI_RUNTIME"
	}
	receipt := model.Receipt{JobID: job.ID, ProfileID: job.Plan.ProfileID, StackID: job.Plan.StackID, Status: status, SourceAttempts: append([]model.SourceAttempt(nil), job.Attempts...), AssetSnapshot: append([]model.AssetPlan(nil), job.Plan.Assets...), GeneratedAt: time.Now().UTC()}
	if err := m.writeReceipt(receipt); err != nil {
		m.update(job, "PARTIAL", err.Error())
		return
	}
	m.update(job, status, "install plan complete")
}

func (m *Manager) update(job *model.Job, state, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.State = state
	job.UpdatedAt = time.Now().UTC()
	if message != "" {
		job.Progress["message"] = message
	}
}

func (m *Manager) updateProgress(job *model.Job, index, total int, asset string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Progress["index"] = index + 1
	job.Progress["total"] = total
	job.Progress["asset"] = asset
	job.UpdatedAt = time.Now().UTC()
}

func (m *Manager) setProgress(job *model.Job, key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Progress[key] = value
	job.UpdatedAt = time.Now().UTC()
}

func (m *Manager) appendAttempts(job *model.Job, attempts ...model.SourceAttempt) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Attempts = append(job.Attempts, attempts...)
	job.UpdatedAt = time.Now().UTC()
}

func (m *Manager) writeReceipt(receipt model.Receipt) error {
	if m.StateDir == "" {
		return nil
	}
	dir := filepath.Join(m.StateDir, "receipts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, receipt.JobID+".json"), data, 0o644)
}

func (m *Manager) ReadReceipt(id string) (model.Receipt, error) {
	if m.StateDir == "" {
		return model.Receipt{}, fmt.Errorf("state directory not configured")
	}
	data, err := os.ReadFile(filepath.Join(m.StateDir, "receipts", id+".json"))
	if err != nil {
		return model.Receipt{}, err
	}
	var receipt model.Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return model.Receipt{}, err
	}
	return receipt, nil
}

func findProfile(m model.Manifest, id string) (model.Profile, bool) {
	for _, p := range m.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return model.Profile{}, false
}

func findAsset(m model.Manifest, id string) (model.Asset, bool) {
	for _, a := range m.Assets {
		if a.ID == id {
			return a, true
		}
	}
	return model.Asset{}, false
}

func findInstance(instances []model.Instance, id string) (model.Instance, bool) {
	if id != "" {
		for _, instance := range instances {
			if instance.ID == id {
				return instance, true
			}
		}
	}
	if len(instances) > 0 {
		return instances[0], true
	}
	return model.Instance{}, false
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func withinRoot(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
