package detect

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"h3oneclick.local/launcher/internal/model"
)

type Detector struct {
	CommandTimeout time.Duration
}

func New() *Detector {
	return &Detector{CommandTimeout: 10 * time.Second}
}

func (d *Detector) Scan(ctx context.Context, explicitPaths []string, explicitPorts []int) model.ScanResult {
	started := time.Now()
	hw := d.hardware(ctx)
	instances := d.instances(ctx, explicitPaths, explicitPorts)
	result := model.ScanResult{Hardware: hw, Instances: instances}
	if hw.Backend == "unknown" {
		result.Warnings = append(result.Warnings, "未识别到 CUDA/MPS/ROCm 后端；自动推荐会保持保守")
	}
	if len(instances) == 0 {
		result.Warnings = append(result.Warnings, "未发现现有 ComfyUI；可在安装计划中创建隔离实例")
	}
	if time.Since(started) > 60*time.Second {
		result.Warnings = append(result.Warnings, "扫描超过 60 秒，部分结果可能来自超时回退")
	}
	return result
}

func (d *Detector) hardware(ctx context.Context) model.HardwareSnapshot {
	now := time.Now().UTC()
	h := model.HardwareSnapshot{
		OS:         platformName(),
		Arch:       runtime.GOARCH,
		Backend:    "unknown",
		CapturedAt: now,
	}
	h.RAMGiB = detectRAM(ctx, d.CommandTimeout)
	h.DiskFreeGiB = detectDiskFree(".")
	if ff := commandVersion(ctx, d.CommandTimeout, "ffmpeg", "-version"); ff != "" {
		h.FFmpeg = strings.Fields(ff)[0]
	}
	if gpus := detectNVIDIA(ctx, d.CommandTimeout); len(gpus) > 0 {
		h.GPUs = gpus
		h.GPUName = gpus[0].Name
		h.Backend = "cuda"
		for _, g := range gpus {
			h.VRAMGiB += g.VRAMGiB
			h.VRAMFreeGiB += g.VRAMFreeGiB
			if h.ComputeCapability == "" {
				h.ComputeCapability = g.ComputeCap
			}
		}
		if v := commandVersion(ctx, d.CommandTimeout, "nvidia-smi", "--query-gpu=driver_version", "--format=csv,noheader"); v != "" {
			h.Driver = strings.TrimSpace(strings.Split(v, "\n")[0])
		}
	} else if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		h.Backend = "mps"
		h.UnifiedMemoryGiB = detectMacMemory(ctx, d.CommandTimeout)
		h.UnifiedMemoryFreeGiB = h.UnifiedMemoryGiB
		h.GPUName = detectMacGPU(ctx, d.CommandTimeout)
	}
	h.ID = hardwareID(h)
	return h
}

func detectNVIDIA(ctx context.Context, timeout time.Duration) []model.GPUInfo {
	out, err := run(ctx, timeout, "nvidia-smi", "--query-gpu=name,memory.total,memory.free,compute_cap", "--format=csv,noheader,nounits")
	if err != nil {
		return nil
	}
	return ParseNVIDIAOutput(out)
}

// ParseNVIDIAOutput is exported so fixture-based tests do not require a GPU.
func ParseNVIDIAOutput(out string) []model.GPUInfo {
	var result []model.GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		vram, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		free, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		result = append(result, model.GPUInfo{
			Name:        strings.TrimSpace(parts[0]),
			Backend:     "cuda",
			VRAMGiB:     vram / 1024,
			VRAMFreeGiB: free / 1024,
			ComputeCap:  strings.TrimSpace(parts[3]),
		})
	}
	return result
}

func detectRAM(ctx context.Context, timeout time.Duration) float64 {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/meminfo")
		if err == nil {
			re := regexp.MustCompile(`(?m)^MemTotal:\s+(\d+)\s+kB`)
			m := re.FindStringSubmatch(string(data))
			if len(m) == 2 {
				v, _ := strconv.ParseFloat(m[1], 64)
				return v / 1024 / 1024
			}
		}
	}
	if runtime.GOOS == "darwin" {
		return detectMacMemory(ctx, timeout)
	}
	if runtime.GOOS == "windows" {
		out, err := run(ctx, timeout, "powershell", "-NoProfile", "-Command", "[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1GB,2)")
		if err == nil {
			v, _ := strconv.ParseFloat(strings.TrimSpace(out), 64)
			return v
		}
	}
	return 0
}

func detectMacMemory(ctx context.Context, timeout time.Duration) float64 {
	out, err := run(ctx, timeout, "sysctl", "-n", "hw.memsize")
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(out), 64)
	return v / 1024 / 1024 / 1024
}

func detectMacGPU(ctx context.Context, timeout time.Duration) string {
	out, err := run(ctx, timeout, "system_profiler", "SPDisplaysDataType")
	if err != nil {
		return "Apple Silicon"
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(line), "chipset model") {
			if p := strings.SplitN(line, ":", 2); len(p) == 2 {
				return strings.TrimSpace(p[1])
			}
		}
	}
	return "Apple Silicon"
}

func detectDiskFree(path string) float64 {
	out, err := exec.Command("df", "-kP", path).Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[3], 64)
	return v / 1024 / 1024
}

func (d *Detector) instances(ctx context.Context, explicit []string, explicitPorts []int) []model.Instance {
	paths := append([]string{}, explicit...)
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, "ComfyUI"),
			filepath.Join(home, "ComfyUI_windows_portable"),
			filepath.Join(home, "AI", "ComfyUI"),
			filepath.Join(home, "AI", "ComfyUI_windows_portable"),
		)
	}
	paths = append(paths, filepath.Join(".", "ComfyUI"), filepath.Join(".", "ComfyUI_windows_portable"))
	seen := map[string]bool{}
	var result []model.Instance
	ports := normalizePorts(explicitPorts)
	for _, p := range paths {
		p, _ = filepath.Abs(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		if inst, ok := inspectInstance(ctx, p, ports); ok {
			result = append(result, inst)
		}
	}
	return result
}

func inspectInstance(ctx context.Context, path string, ports []int) (model.Instance, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return model.Instance{}, false
	}
	mainPy := filepath.Join(path, "main.py")
	portablePython := filepath.Join(path, "python_embeded", "python.exe")
	venvPython := filepath.Join(path, "venv", "bin", "python")
	if !exists(mainPy) && !exists(portablePython) && !exists(venvPython) && !exists(filepath.Join(path, "custom_nodes")) {
		return model.Instance{}, false
	}
	kind := "source"
	if exists(portablePython) {
		kind = "portable"
	} else if exists(venvPython) {
		kind = "venv"
	}
	port := 0
	running := false
	for _, p := range ports {
		if probeComfyPort(ctx, p) {
			port = p
			running = true
			break
		}
	}
	var nodes []string
	custom := filepath.Join(path, "custom_nodes")
	if entries, err := os.ReadDir(custom); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				nodes = append(nodes, entry.Name())
			}
		}
	}
	compat := "unknown"
	if exists(mainPy) && (exists(portablePython) || exists(venvPython)) {
		compat = "repairable"
	}
	return model.Instance{
		ID:            instanceID(path),
		Path:          path,
		Kind:          kind,
		Platform:      platformName(),
		Port:          port,
		Running:       running,
		ModelRoots:    []string{filepath.Join(path, "models")},
		NodeRoots:     []string{custom},
		Nodes:         nodes,
		Compatibility: compat,
		LastSeenAt:    time.Now().UTC(),
	}, true
}

func normalizePorts(explicit []int) []int {
	seen := map[int]bool{}
	ports := make([]int, 0, len(explicit)+12)
	for _, p := range explicit {
		if p > 0 && p < 65536 && !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	for p := 8188; p <= 8198; p++ {
		if !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	if !seen[18188] {
		ports = append(ports, 18188)
	}
	return ports
}

func probeComfyPort(ctx context.Context, port int) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 650*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/system_stats", port), nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 500 {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	text := strings.ToLower(string(body))
	return strings.Contains(text, "comfyui_version") || strings.Contains(text, "\"system\"")
}

func platformName() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

func instanceID(path string) string {
	h := sha1.Sum([]byte(path))
	return "inst-" + hex.EncodeToString(h[:])[:12]
}

func hardwareID(h model.HardwareSnapshot) string {
	value := strings.Join([]string{h.OS, h.Arch, h.GPUName, h.Backend, h.ComputeCapability}, "|")
	hash := sha1.Sum([]byte(value))
	return "hw-" + hex.EncodeToString(hash[:])[:12]
}

func commandVersion(ctx context.Context, timeout time.Duration, name string, args ...string) string {
	out, err := run(ctx, timeout, name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func run(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if ctx.Err() != nil {
		return buf.String(), ctx.Err()
	}
	return buf.String(), err
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadVersion is intentionally small and side-effect free for the UI inventory.
func ReadVersion(ctx context.Context, path string) string {
	python := filepath.Join(path, "venv", "bin", "python")
	if runtime.GOOS == "windows" {
		python = filepath.Join(path, "python_embeded", "python.exe")
	}
	if !exists(python) {
		python = "python3"
	}
	out, err := run(ctx, 5*time.Second, python, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ParsePort is a small utility for tests and future log readers.
func ParsePort(line string) int {
	scanner := bufio.NewScanner(strings.NewReader(line))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		for _, p := range parts {
			if strings.HasPrefix(p, "--port=") {
				v, _ := strconv.Atoi(strings.TrimPrefix(p, "--port="))
				return v
			}
		}
	}
	return 0
}
