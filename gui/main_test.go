package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	wailsassets "github.com/wailsapp/wails/v2/pkg/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"h3oneclick.local/launcher/internal/manifest"
	"h3oneclick.local/launcher/internal/server"
)

type noopLogger struct{}

func (noopLogger) Debug(string, ...interface{}) {}
func (noopLogger) Error(string, ...interface{}) {}

// The GUI relies on AssetServer.Handler fallthrough for /api/*.
// This test exercises the exact composition main.go wires up.
func TestAssetServerAPIFallthrough(t *testing.T) {
	m, err := manifest.Load("")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	dir := t.TempDir()
	srv := server.New(m, dir)

	h, err := wailsassets.NewAssetHandler(assetserver.Options{
		Assets:  assets,
		Handler: srv.Handler(),
	}, noopLogger{})
	if err != nil {
		t.Fatalf("asset handler: %v", err)
	}

	// GET / must serve the embedded GUI index, not fall through to the CLI web UI
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "CONTROL DECK") {
		t.Fatalf("GET / = %d, want GUI index with CONTROL DECK", rec.Code)
	}

	// GET /api/health must fall through to the engine handler
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("GET /api/health = %d %s", rec.Code, rec.Body.String())
	}

	// POST must reach the engine (asset server passes non-GET straight through)
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader(`{"paths":[],"ports":[18188]}`))
	req.Header.Set("content-type", "application/json")
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	if rec.Code != 200 || !strings.Contains(string(body), `"hardware"`) {
		t.Fatalf("POST /api/scan = %d %s", rec.Code, body[:min(200, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
