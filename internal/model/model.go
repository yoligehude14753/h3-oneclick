package model

import "time"

// HardwareSnapshot is the read-only machine snapshot used by the resolver.
type HardwareSnapshot struct {
	ID                   string    `json:"id"`
	OS                   string    `json:"os"`
	Arch                 string    `json:"arch"`
	GPUName              string    `json:"gpu_name"`
	Backend              string    `json:"backend"`
	ComputeCapability    string    `json:"compute_capability,omitempty"`
	VRAMGiB              float64   `json:"vram_gib,omitempty"`
	VRAMFreeGiB          float64   `json:"vram_free_gib,omitempty"`
	UnifiedMemoryGiB     float64   `json:"unified_memory_gib,omitempty"`
	UnifiedMemoryFreeGiB float64   `json:"unified_memory_free_gib,omitempty"`
	RAMGiB               float64   `json:"ram_gib,omitempty"`
	DiskFreeGiB          float64   `json:"disk_free_gib,omitempty"`
	Driver               string    `json:"driver,omitempty"`
	FFmpeg               string    `json:"ffmpeg,omitempty"`
	GPUs                 []GPUInfo `json:"gpus,omitempty"`
	CapturedAt           time.Time `json:"captured_at"`
}

type GPUInfo struct {
	Name        string  `json:"name"`
	Backend     string  `json:"backend"`
	VRAMGiB     float64 `json:"vram_gib,omitempty"`
	VRAMFreeGiB float64 `json:"vram_free_gib,omitempty"`
	ComputeCap  string  `json:"compute_capability,omitempty"`
}

type Instance struct {
	ID            string    `json:"id"`
	Path          string    `json:"path"`
	Kind          string    `json:"kind"`
	Platform      string    `json:"platform"`
	ComfyVersion  string    `json:"comfy_version,omitempty"`
	PythonVersion string    `json:"python_version,omitempty"`
	Port          int       `json:"port,omitempty"`
	Running       bool      `json:"running"`
	ModelRoots    []string  `json:"model_roots,omitempty"`
	NodeRoots     []string  `json:"node_roots,omitempty"`
	Nodes         []string  `json:"nodes,omitempty"`
	Compatibility string    `json:"compatibility"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

type ProbeError struct {
	Item    string `json:"item"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Recover string `json:"recover,omitempty"`
}

type ScanResult struct {
	Hardware  HardwareSnapshot `json:"hardware"`
	Instances []Instance       `json:"instances"`
	Warnings  []string         `json:"warnings,omitempty"`
	Errors    []ProbeError     `json:"errors,omitempty"`
}

type Requirements struct {
	Platforms            []string `json:"platforms,omitempty"`
	Backends             []string `json:"backends,omitempty"`
	MinFreeVRAMGiB       *float64 `json:"min_free_vram_gib,omitempty"`
	MinRAMGiB            *float64 `json:"min_ram_gib,omitempty"`
	ComputeCapabilityMin *float64 `json:"compute_capability_min,omitempty"`
	ComfyVersionMin      string   `json:"comfy_version_min,omitempty"`
	RequiredNodes        []string `json:"required_nodes,omitempty"`
	RequiredLoaders      []string `json:"required_loaders,omitempty"`
	Runtime              string   `json:"runtime,omitempty"`
	RequiresComfy        bool     `json:"requires_comfy,omitempty"`
	RequiresAppleSilicon bool     `json:"requires_apple_silicon,omitempty"`
	PublishedMemoryHint  string   `json:"published_memory_hint,omitempty"`
}

type Source struct {
	ID              string   `json:"id"`
	AssetID         string   `json:"asset_id"`
	Type            string   `json:"source_type"`
	Role            string   `json:"source_role"`
	Repository      string   `json:"repository_or_share"`
	Revision        string   `json:"revision_or_version"`
	FilePath        string   `json:"file_path_or_id"`
	URL             string   `json:"url,omitempty"`
	Transport       string   `json:"transport"`
	SizeBytes       int64    `json:"size_bytes,omitempty"`
	SHA256          string   `json:"sha256,omitempty"`
	Dependencies    []string `json:"dependency_ids,omitempty"`
	MirrorOfAssetID string   `json:"mirror_of_asset_id,omitempty"`
	FallbackOrder   int      `json:"fallback_order"`
	ProbeStatus     string   `json:"probe_status"`
	Status          string   `json:"status"`
}

type Asset struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         string       `json:"kind"`
	Version      string       `json:"version"`
	Platforms    []string     `json:"platforms,omitempty"`
	Backends     []string     `json:"backends,omitempty"`
	TargetPath   string       `json:"target_path"`
	SizeBytes    int64        `json:"size_bytes,omitempty"`
	Requires     Requirements `json:"requires,omitempty"`
	SourceStatus string       `json:"source_status"`
	Sources      []Source     `json:"sources"`
}

type QualityGate struct {
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Seconds  float64 `json:"seconds,omitempty"`
	Audio    bool    `json:"audio"`
	Verified bool    `json:"verified"`
}

type Profile struct {
	ID               string       `json:"id"`
	Label            string       `json:"label"`
	RuntimeType      string       `json:"runtime_type"`
	SourceTier       string       `json:"source_tier"`
	Platforms        []string     `json:"platforms,omitempty"`
	Backends         []string     `json:"backends,omitempty"`
	StackIDs         []string     `json:"stack_ids,omitempty"`
	Assets           []string     `json:"assets"`
	WorkflowAssetID  string       `json:"workflow_asset_id,omitempty"`
	Sampler          string       `json:"sampler,omitempty"`
	Steps            []int        `json:"steps,omitempty"`
	Requires         Requirements `json:"requires"`
	QualityGate      QualityGate  `json:"quality_gate"`
	FallbackProfiles []string     `json:"fallback_profiles,omitempty"`
	Auto             bool         `json:"auto"`
	ResearchOnly     bool         `json:"research_only"`
}

type Manifest struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   string    `json:"generated_at"`
	Assets        []Asset   `json:"assets"`
	Profiles      []Profile `json:"profiles"`
}

type WorkloadRequest struct {
	Task           string  `json:"task"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Seconds        float64 `json:"seconds"`
	Audio          bool    `json:"audio"`
	PreferredSteps int     `json:"preferred_steps"`
}

type Preference struct {
	PreferredSteps int    `json:"preferred_steps"`
	QualityFloor   string `json:"quality_floor"`
}

type SourcePolicy struct {
	AllowSameFileMirror        bool `json:"allow_same_file_mirror"`
	AllowCompatibleAlternative bool `json:"allow_compatible_alternative"`
}

type BestConfigRequest struct {
	HardwareID      string            `json:"hardware_id,omitempty"`
	InstanceID      string            `json:"instance_id,omitempty"`
	Hardware        *HardwareSnapshot `json:"hardware,omitempty"`
	Workload        WorkloadRequest   `json:"workload"`
	Preference      Preference        `json:"preference"`
	SourcePolicy    SourcePolicy      `json:"source_policy"`
	OverrideProfile string            `json:"override_profile,omitempty"`
}

type Alternative struct {
	ProfileID  string   `json:"profile"`
	StackID    string   `json:"stack_id,omitempty"`
	Status     string   `json:"status"`
	Reasons    []string `json:"reasons"`
	NewAssets  []string `json:"new_assets,omitempty"`
	SourceTier string   `json:"source_tier,omitempty"`
}

type BestConfigDecision struct {
	PreferredSteps     int           `json:"preferred_steps"`
	RecommendedProfile string        `json:"recommended_profile"`
	RecommendedStack   string        `json:"recommended_stack,omitempty"`
	ReasonCodes        []string      `json:"reason_codes"`
	Alternatives       []Alternative `json:"alternatives"`
	UserOverride       string        `json:"user_override,omitempty"`
	EffectiveProfile   string        `json:"effective_profile"`
	FallbackChain      []string      `json:"fallback_chain,omitempty"`
	SourcePolicy       SourcePolicy  `json:"source_policy"`
	ComputedAt         time.Time     `json:"computed_at"`
}

type SourceAttempt struct {
	JobID      string `json:"job_id,omitempty"`
	AssetID    string `json:"asset_id"`
	SourceID   string `json:"source_id"`
	AttemptNo  int    `json:"attempt_no"`
	Probe      string `json:"probe"`
	HTTPStatus int    `json:"http_status,omitempty"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	ErrorCode  string `json:"error_code,omitempty"`
	Selected   bool   `json:"selected"`
}

type SourceProbeResult struct {
	AssetID            string          `json:"asset_id"`
	Attempts           []SourceAttempt `json:"attempts"`
	SelectedSourceRole string          `json:"selected_source_role,omitempty"`
	MappingStatus      string          `json:"mapping_status"`
	NextAction         string          `json:"next_action"`
}

type PlanRequest struct {
	InstanceID       string              `json:"instance_id,omitempty"`
	ProfileID        string              `json:"profile_id,omitempty"`
	Decision         *BestConfigDecision `json:"decision,omitempty"`
	Hardware         *HardwareSnapshot   `json:"hardware,omitempty"`
	Workload         WorkloadRequest     `json:"workload"`
	SourcePolicy     SourcePolicy        `json:"source_policy"`
	TargetDir        string              `json:"target_dir,omitempty"`
	BootstrapRuntime bool                `json:"bootstrap_runtime"`
	DryRun           bool                `json:"dry_run"`
}

type AssetPlan struct {
	AssetID    string   `json:"asset_id"`
	TargetPath string   `json:"target_path"`
	SizeBytes  int64    `json:"size_bytes,omitempty"`
	Reuse      bool     `json:"reuse"`
	Primary    Source   `json:"primary"`
	Mirrors    []Source `json:"mirrors,omitempty"`
}

type InstallPlan struct {
	Platform         string       `json:"platform"`
	Backend          string       `json:"backend"`
	InstanceMode     string       `json:"instance_mode"`
	InstanceID       string       `json:"instance_id,omitempty"`
	ProfileID        string       `json:"profile"`
	StackID          string       `json:"stack_id,omitempty"`
	PreferredSteps   int          `json:"preferred_steps"`
	Assets           []AssetPlan  `json:"assets"`
	RequiredFreeGiB  float64      `json:"required_free_gib"`
	AvailableFreeGiB float64      `json:"available_free_gib"`
	Port             int          `json:"port,omitempty"`
	SourcePolicy     SourcePolicy `json:"source_policy"`
	RuntimeBootstrap bool         `json:"runtime_bootstrap"`
	RuntimeRoot      string       `json:"runtime_root,omitempty"`
	RuntimeSource    string       `json:"runtime_source,omitempty"`
	ComfyRoot        string       `json:"comfy_root,omitempty"`
	TemplateAdapter  string       `json:"template_adapter,omitempty"`
	Warnings         []string     `json:"warnings,omitempty"`
	Blocked          bool         `json:"blocked"`
	BlockReasons     []string     `json:"block_reasons,omitempty"`
}

type Job struct {
	ID        string          `json:"id"`
	State     string          `json:"state"`
	Plan      InstallPlan     `json:"plan"`
	Progress  map[string]any  `json:"progress,omitempty"`
	Attempts  []SourceAttempt `json:"attempts,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Receipt struct {
	JobID          string          `json:"job_id"`
	ProfileID      string          `json:"profile_id"`
	StackID        string          `json:"stack_id,omitempty"`
	Status         string          `json:"status"`
	SourceAttempts []SourceAttempt `json:"source_attempts,omitempty"`
	AssetSnapshot  []AssetPlan     `json:"asset_snapshot,omitempty"`
	SmokeResult    map[string]any  `json:"smoke_result,omitempty"`
	GeneratedAt    time.Time       `json:"generated_at"`
}

type TemplateSession struct {
	ID               string    `json:"session_id"`
	Status           string    `json:"status"`
	ProfileID        string    `json:"profile_id"`
	InstanceID       string    `json:"instance_id"`
	WorkflowPath     string    `json:"workflow_path"`
	TemplateAssetID  string    `json:"template_asset_id"`
	LaunchURL        string    `json:"launch_url,omitempty"`
	ServerReadback   bool      `json:"server_readback"`
	FrontendReadback bool      `json:"frontend_readback"`
	HasH3            bool      `json:"has_h3"`
	NodeCount        int       `json:"node_count,omitempty"`
	Message          string    `json:"message,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}
