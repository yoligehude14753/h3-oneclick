package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"h3oneclick.local/launcher/internal/manifest"
	"h3oneclick.local/launcher/internal/model"
)

func TestHealthAndEmbeddedUI(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(m, t.TempDir()).Handler())
	defer ts.Close()
	for _, path := range []string{"/api/health", "/"} {
		response, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.StatusCode)
		}
		response.Body.Close()
	}
}

func TestBestConfigEndpoint(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(m, t.TempDir()).Handler())
	defer ts.Close()
	payload := `{"hardware":{"os":"windows","arch":"amd64","backend":"cuda","vram_gib":24,"vram_free_gib":24,"ram_gib":64},"workload":{"task":"t2v","width":768,"height":432,"seconds":5,"audio":true,"preferred_steps":4},"preference":{"preferred_steps":4},"source_policy":{"allow_same_file_mirror":true}}`
	response, err := http.Post(ts.URL+"/api/best-config", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["effective_profile"] != "turbo-4-768" {
		t.Fatalf("unexpected decision: %#v", body)
	}
}

func TestSaveAndReadbackWorkflow(t *testing.T) {
	var stored []byte
	comfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			stored, _ = io.ReadAll(r.Body)
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"path":"workflows/H3.json"}`))
		case http.MethodGet:
			_, _ = w.Write(stored)
		}
	}))
	defer comfy.Close()
	workflow := []byte(`{"nodes":[{"type":"MiniMaxH3"}]}`)
	if err := saveAndReadbackWorkflow(context.Background(), comfy.URL, "workflows/H3.json", workflow); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, workflow) {
		t.Fatalf("stored workflow mismatch")
	}
}

func TestTemplateReadbackLifecycle(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	app := New(m, t.TempDir())
	app.templateSessions["s1"] = model.TemplateSession{ID: "s1", Status: "TEMPLATE_SAVED", WorkflowPath: "workflows/H3.json", UpdatedAt: time.Now()}
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()
	payload := `{"session_id":"s1","status":"TEMPLATE_READY","workflow_path":"workflows/H3.json","has_h3":true,"node_count":6}`
	response, err := http.Post(ts.URL+"/api/template/readback", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	status, err := http.Get(ts.URL + "/api/template/status?session_id=s1")
	if err != nil {
		t.Fatal(err)
	}
	defer status.Body.Close()
	var session model.TemplateSession
	if err := json.NewDecoder(status.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "TEMPLATE_READY" || !session.FrontendReadback || session.NodeCount != 6 {
		t.Fatalf("unexpected template readback: %#v", session)
	}
}

func TestInspectWorkflow(t *testing.T) {
	hasH3, count := inspectWorkflow([]byte(`{"nodes":[{"type":"MiniMaxH3"},{"type":"SaveVideo"}]}`))
	if !hasH3 || count != 2 {
		t.Fatalf("expected H3 workflow, got hasH3=%v count=%d", hasH3, count)
	}
}

func TestTemplateOpenSavesWorkflowAndCreatesSession(t *testing.T) {
	workflow := []byte(`{"nodes":[{"type":"MiniMaxH3"}],"version":0.4}`)
	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("content-length", "49")
			return
		}
		_, _ = w.Write(workflow)
	}))
	defer assetServer.Close()
	var stored []byte
	comfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/system_stats" {
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.3.43"},"devices":[]}`))
			return
		}
		if r.Method == http.MethodPost {
			stored, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"path":"workflows/H3-OneClick-native-20.json"}`))
			return
		}
		_, _ = w.Write(stored)
	}))
	defer comfy.Close()
	m, err := manifest.Load("")
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Assets {
		if m.Assets[i].ID == "h3-workflow-t2v" {
			m.Assets[i].Sources = []model.Source{{ID: "fixture", AssetID: "h3-workflow-t2v", Role: "primary", URL: assetServer.URL + "/workflow", FallbackOrder: 0}}
			m.Assets[i].SizeBytes = int64(len(workflow))
		}
	}
	app := New(m, t.TempDir())
	port := mustTestPort(t, comfy.URL)
	app.lastScan = model.ScanResult{Hardware: model.HardwareSnapshot{ID: "fixture-hw", OS: "linux", Backend: "cuda"}, Instances: []model.Instance{{ID: "i1", Path: t.TempDir(), Port: port, Running: true, Platform: "linux"}}}
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()
	payload := `{"instance_id":"i1","profile_id":"native-20","open_browser":false}`
	response, err := http.Post(ts.URL+"/api/template/open", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session model.TemplateSession
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.Status != "TEMPLATE_SAVED" || !session.ServerReadback || session.FrontendReadback || !bytes.Equal(stored, workflow) {
		t.Fatalf("unexpected template session: %#v stored=%q", session, stored)
	}
}

func mustTestPort(t *testing.T, rawURL string) int {
	t.Helper()
	parts := strings.Split(rawURL, ":")
	if len(parts) == 0 {
		t.Fatal("invalid test URL")
	}
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatal(err)
	}
	return port
}
