package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"h3oneclick.local/launcher/internal/detect"
	"h3oneclick.local/launcher/internal/install"
	"h3oneclick.local/launcher/internal/model"
	"h3oneclick.local/launcher/internal/resolver"
	"h3oneclick.local/launcher/internal/source"
)

type Server struct {
	Manifest         model.Manifest
	Detector         *detect.Detector
	Registry         *source.Registry
	Resolver         *resolver.Resolver
	Planner          *install.Planner
	Manager          *install.Manager
	StateDir         string
	mu               sync.RWMutex
	lastScan         model.ScanResult
	processes        map[string]*exec.Cmd
	templateSessions map[string]model.TemplateSession
}

func New(m model.Manifest, stateDir string) *Server {
	registry := source.NewRegistry(m)
	planner := install.NewPlanner(m, registry)
	return &Server{
		Manifest:         m,
		Detector:         detect.New(),
		Registry:         registry,
		Resolver:         resolver.New(m),
		Planner:          planner,
		Manager:          install.NewManager(planner, registry, stateDir),
		StateDir:         stateDir,
		processes:        make(map[string]*exec.Cmd),
		templateSessions: make(map[string]model.TemplateSession),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(mustSub(webFiles, "web"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := webFiles.ReadFile("web/index.html")
		w.Header().Set("content-type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/scan", s.scan)
	mux.HandleFunc("/api/hardware", s.hardware)
	mux.HandleFunc("/api/instances", s.instances)
	mux.HandleFunc("/api/profiles", s.profiles)
	mux.HandleFunc("/api/best-config", s.bestConfig)
	mux.HandleFunc("/api/sources/probe", s.probeSources)
	mux.HandleFunc("/api/sources/", s.sourceByAsset)
	mux.HandleFunc("/api/plan", s.plan)
	mux.HandleFunc("/api/install", s.install)
	mux.HandleFunc("/api/install/", s.installByID)
	mux.HandleFunc("/api/receipts/", s.receipt)
	mux.HandleFunc("/api/template/open", s.templateOpen)
	mux.HandleFunc("/api/template/status", s.templateStatus)
	mux.HandleFunc("/api/template/readback", s.templateReadback)
	mux.HandleFunc("/api/runtime/open", s.runtimeOpen)
	mux.HandleFunc("/api/launch", s.launch)
	return withCORS(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.Manifest.SchemaVersion, "runtime": runtime.Version()})
}

func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Paths []string `json:"paths"`
		Ports []int    `json:"ports"`
	}
	if r.Method == http.MethodPost && r.Body != nil {
		_ = decodeJSON(r, &request)
	}
	result := s.Detector.Scan(r.Context(), request.Paths, request.Ports)
	s.mu.Lock()
	s.lastScan = result
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) hardware(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastScan.Hardware.ID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"hardware": nil, "message": "scan required"})
		return
	}
	writeJSON(w, http.StatusOK, s.lastScan.Hardware)
}

func (s *Server) instances(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, s.lastScan.Instances)
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Manifest.Profiles)
}

func (s *Server) bestConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request model.BestConfigRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	snapshot := s.snapshot(request.Hardware)
	decision := s.Resolver.Resolve(snapshot.Hardware, snapshot.Instances, request)
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) probeSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request struct {
		AssetID string             `json:"asset_id"`
		Policy  model.SourcePolicy `json:"source_policy"`
	}
	if err := decodeJSON(r, &request); err != nil || request.AssetID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "asset_id is required")
		return
	}
	result := s.Registry.Probe(r.Context(), request.AssetID, request.Policy)
	status := http.StatusOK
	if result.MappingStatus == "blocked" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, result)
}

func (s *Server) sourceByAsset(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sources/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid_asset", "asset id is required")
		return
	}
	asset, ok := s.Registry.Asset(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "asset not found")
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request model.PlanRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	snapshot := s.snapshot(request.Hardware)
	decision := model.BestConfigDecision{}
	if request.Decision != nil {
		decision = *request.Decision
	} else {
		bestReq := model.BestConfigRequest{Hardware: &snapshot.Hardware, InstanceID: request.InstanceID, Workload: request.Workload, Preference: model.Preference{PreferredSteps: request.Workload.PreferredSteps}, SourcePolicy: request.SourcePolicy, OverrideProfile: request.ProfileID}
		decision = s.Resolver.Resolve(snapshot.Hardware, snapshot.Instances, bestReq)
	}
	plan, err := s.Planner.BuildPlan(request, decision, snapshot)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "plan_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request struct {
		Plan   model.InstallPlan `json:"plan"`
		DryRun bool              `json:"dry_run"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if request.Plan.ProfileID == "" {
		writeError(w, http.StatusUnprocessableEntity, "plan_required", "plan.profile is required")
		return
	}
	job := s.Manager.Start(r.Context(), request.Plan, request.DryRun)
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) installByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/install/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_job", "job id is required")
		return
	}
	job, ok := s.Manager.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) receipt(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/receipts/")
	receipt, err := s.Manager.ReadReceipt(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request struct {
		InstanceID  string `json:"instance_id"`
		OpenBrowser bool   `json:"open_browser"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	snapshot := s.snapshot(nil)
	instance, ok := chooseInstance(snapshot.Instances, request.InstanceID)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "instance_required", "no ComfyUI instance found")
		return
	}
	result := s.launchInstance(r.Context(), instance, request.OpenBrowser)
	writeJSON(w, result.HTTPStatus, result.Body)
}

func (s *Server) templateOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var request struct {
		InstanceID  string `json:"instance_id"`
		ProfileID   string `json:"profile_id"`
		OpenBrowser bool   `json:"open_browser"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	profile, ok := findProfile(s.Manifest, request.ProfileID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if profile.RuntimeType != "comfyui" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "NON_COMFYUI_RUNTIME", "profile_id": profile.ID, "readback": false, "message": "external runtime does not select a ComfyUI template"})
		return
	}
	snapshot := s.snapshot(nil)
	instance, ok := chooseInstance(snapshot.Instances, request.InstanceID)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "instance_required", "no ComfyUI instance found")
		return
	}
	result := s.launchInstance(r.Context(), instance, false)
	if result.HTTPStatus != http.StatusOK {
		writeJSON(w, result.HTTPStatus, result.Body)
		return
	}
	baseURL := result.Body.(map[string]any)["url"].(string)
	workflowData, err := s.workflowData(r.Context(), profile.WorkflowAssetID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "workflow_download_failed", err.Error())
		return
	}
	hasH3, nodeCount := inspectWorkflow(workflowData)
	if !hasH3 {
		writeError(w, http.StatusUnprocessableEntity, "workflow_not_h3", "workflow does not contain an H3 graph")
		return
	}
	workflowPath := "workflows/H3-OneClick-" + safeName(profile.ID) + ".json"
	if err := saveAndReadbackWorkflow(r.Context(), baseURL, workflowPath, workflowData); err != nil {
		writeError(w, http.StatusBadGateway, "workflow_store_failed", err.Error())
		return
	}
	sessionID := fmt.Sprintf("template-%d", time.Now().UnixNano())
	callback := "http://" + r.Host + "/api/template/readback"
	query := url.Values{}
	query.Set("h3_autoload", "1")
	query.Set("h3_workflow", workflowPath)
	query.Set("h3_session", sessionID)
	query.Set("h3_callback", callback)
	launchURL := baseURL + "/?" + query.Encode()
	session := model.TemplateSession{
		ID: sessionID, Status: "TEMPLATE_SAVED", ProfileID: profile.ID,
		InstanceID: instance.ID, WorkflowPath: workflowPath, TemplateAssetID: profile.WorkflowAssetID,
		LaunchURL: launchURL, ServerReadback: true, FrontendReadback: false,
		HasH3: hasH3, NodeCount: nodeCount,
		Message:   "workflow saved and read back; frontend adapter must report TEMPLATE_READY",
		UpdatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	s.templateSessions[sessionID] = session
	s.mu.Unlock()
	if request.OpenBrowser {
		_ = openURL(launchURL)
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) templateStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "session_required", "session_id is required")
		return
	}
	s.mu.RLock()
	session, ok := s.templateSessions[id]
	s.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "template session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) templateReadback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var payload struct {
		SessionID    string `json:"session_id"`
		Status       string `json:"status"`
		WorkflowPath string `json:"workflow_path"`
		HasH3        bool   `json:"has_h3"`
		NodeCount    int    `json:"node_count"`
		Message      string `json:"message"`
	}
	if err := decodeJSON(r, &payload); err != nil || payload.SessionID == "" {
		writeError(w, http.StatusBadRequest, "invalid_readback", "session_id and JSON body are required")
		return
	}
	s.mu.Lock()
	session, ok := s.templateSessions[payload.SessionID]
	if ok {
		if payload.WorkflowPath != "" && payload.WorkflowPath != session.WorkflowPath {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "workflow_path_mismatch", "readback workflow does not match session")
			return
		}
		session.Status = payload.Status
		if session.Status == "" {
			session.Status = "TEMPLATE_BLOCKED"
		}
		session.FrontendReadback = session.Status == "TEMPLATE_READY" && payload.HasH3
		session.HasH3 = payload.HasH3
		session.NodeCount = payload.NodeCount
		session.Message = payload.Message
		session.UpdatedAt = time.Now().UTC()
		s.templateSessions[payload.SessionID] = session
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "template session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) workflowData(ctx context.Context, assetID string) ([]byte, error) {
	asset, ok := s.Registry.Asset(assetID)
	if !ok {
		return nil, fmt.Errorf("workflow asset %q not found", assetID)
	}
	dir := filepath.Join(s.StateDir, "cache", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	destination := filepath.Join(dir, filepath.Base(asset.TargetPath))
	if data, err := os.ReadFile(destination); err == nil {
		if hasH3, _ := inspectWorkflow(data); hasH3 {
			return data, nil
		}
	}
	_, _, err := s.Registry.DownloadAsset(ctx, assetID, destination, model.SourcePolicy{AllowSameFileMirror: true})
	if err != nil {
		return nil, err
	}
	return os.ReadFile(destination)
}

func inspectWorkflow(data []byte) (bool, int) {
	var workflow struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(data, &workflow); err != nil {
		return false, 0
	}
	text := strings.ToLower(string(data))
	return strings.Contains(text, "minimaxh3") || strings.Contains(text, "minimax_h3"), len(workflow.Nodes)
}

func saveAndReadbackWorkflow(ctx context.Context, baseURL, workflowPath string, data []byte) error {
	client := &http.Client{Timeout: 20 * time.Second}
	endpoint := strings.TrimRight(baseURL, "/") + "/userdata/" + url.PathEscape(workflowPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?overwrite=true&full_info=true", bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("ComfyUI userdata POST returned %d", response.StatusCode)
	}
	readRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	readResponse, err := client.Do(readRequest)
	if err != nil {
		return err
	}
	readback, readErr := io.ReadAll(io.LimitReader(readResponse.Body, 4<<20))
	readResponse.Body.Close()
	if readErr != nil {
		return readErr
	}
	if readResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("ComfyUI userdata GET returned %d", readResponse.StatusCode)
	}
	expected := sha256.Sum256(data)
	actual := sha256.Sum256(readback)
	if expected != actual {
		return fmt.Errorf("workflow server readback hash mismatch")
	}
	return nil
}

func safeName(value string) string {
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte('-')
		}
	}
	if out.Len() == 0 {
		return "profile"
	}
	return out.String()
}

func (s *Server) runtimeOpen(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "NON_COMFYUI_RUNTIME", "readback": false, "message": "external runtime opener is a profile hook; no ComfyUI template is selected"})
}

type launchResult struct {
	HTTPStatus int
	Body       any
}

func (s *Server) launchInstance(ctx context.Context, instance model.Instance, openBrowser bool) launchResult {
	port := instance.Port
	if port == 0 {
		port = availablePort(8188, 8198)
	}
	if port == 0 {
		return launchResult{http.StatusConflict, map[string]any{"error": "port_unavailable", "message": "no free port in 8188-8198"}}
	}
	if !instance.Running {
		python := filepath.Join(instance.Path, "venv", "bin", "python")
		if runtime.GOOS == "windows" {
			python = filepath.Join(instance.Path, "python_embeded", "python.exe")
		}
		if _, err := os.Stat(python); err != nil {
			python = "python3"
		}
		cmd := exec.CommandContext(context.Background(), python, filepath.Join(instance.Path, "main.py"), "--listen", "127.0.0.1", "--port", strconv.Itoa(port))
		cmd.Dir = instance.Path
		if err := cmd.Start(); err != nil {
			return launchResult{http.StatusBadGateway, map[string]any{"error": "comfy_start_failed", "message": err.Error()}}
		}
		s.mu.Lock()
		s.processes[instance.ID] = cmd
		s.mu.Unlock()
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	health := waitHTTP(ctx, url+"/system_stats", 20*time.Second)
	if !health {
		return launchResult{http.StatusGatewayTimeout, map[string]any{"error": "comfy_health_timeout", "message": "ComfyUI process started but /system_stats did not respond", "url": url}}
	}
	if openBrowser {
		_ = openURL(url)
	}
	return launchResult{http.StatusOK, map[string]any{"status": "COMFYUI_READY", "url": url, "port": port, "instance_id": instance.ID, "readback": true}}
}

func (s *Server) snapshot(hardware *model.HardwareSnapshot) model.ScanResult {
	s.mu.RLock()
	snapshot := s.lastScan
	s.mu.RUnlock()
	if hardware != nil {
		snapshot.Hardware = *hardware
		if snapshot.Hardware.ID == "" {
			snapshot.Hardware.ID = "request-hardware"
		}
		return snapshot
	}
	if snapshot.Hardware.ID == "" {
		snapshot = s.Detector.Scan(context.Background(), nil, nil)
		s.mu.Lock()
		s.lastScan = snapshot
		s.mu.Unlock()
	}
	return snapshot
}

func chooseInstance(instances []model.Instance, id string) (model.Instance, bool) {
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

func findProfile(m model.Manifest, id string) (model.Profile, bool) {
	for _, p := range m.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return model.Profile{}, false
}

func availablePort(start, end int) int {
	for p := start; p <= end; p++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 100*time.Millisecond)
		if err != nil {
			return p
		}
		conn.Close()
	}
	return 0
}

func waitHTTP(ctx context.Context, url string, timeout time.Duration) bool {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		request, _ := http.NewRequestWithContext(deadline, http.MethodGet, url, nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode < 500 {
				return true
			}
		}
		select {
		case <-deadline.Done():
			return false
		case <-time.After(350 * time.Millisecond):
		}
	}
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported browser platform")
	}
	return cmd.Start()
}

func mustSub(fsys fs.FS, path string) http.FileSystem {
	// This function is replaced below by the typed helper; keeping all embed setup
	// in one file makes the handler easy to test.
	sub, err := fs.Sub(fsys, path)
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": code, "message": message, "timestamp": time.Now().UTC()})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("access-control-allow-origin", "*")
		w.Header().Set("access-control-allow-headers", "content-type")
		w.Header().Set("access-control-allow-methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
