package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"h3oneclick.local/launcher/internal/model"
)

func TestDownloadAssetFallsBackToSameFileMirror(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()
	registry := NewRegistry(model.Manifest{Assets: []model.Asset{{
		ID: "a", Name: "fixture", Kind: "model", SizeBytes: 5,
		Sources: []model.Source{
			{ID: "primary", AssetID: "a", Role: "primary", URL: server.URL + "/primary", FallbackOrder: 0, Status: "verified"},
			{ID: "mirror", AssetID: "a", Role: "same_file_mirror", URL: server.URL + "/mirror", FallbackOrder: 1, Status: "verified"},
		},
	}}})
	dir := t.TempDir()
	dest := filepath.Join(dir, "asset.bin")
	attempts, selected, err := registry.DownloadAsset(context.Background(), "a", dest, model.SourcePolicy{AllowSameFileMirror: true})
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "mirror" || len(attempts) != 2 || !attempts[1].Selected {
		t.Fatalf("unexpected fallback result: selected=%#v attempts=%#v", selected, attempts)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "hello" {
		t.Fatalf("downloaded data mismatch: %q %v", data, err)
	}
}

func TestProbeBlocksWhenPolicyDisallowsMirror(t *testing.T) {
	registry := NewRegistry(model.Manifest{Assets: []model.Asset{{
		ID: "a", Sources: []model.Source{{ID: "mirror", AssetID: "a", Role: "same_file_mirror", URL: "http://127.0.0.1:1", FallbackOrder: 1}},
	}}})
	result := registry.Probe(context.Background(), "a", model.SourcePolicy{AllowSameFileMirror: false})
	if result.MappingStatus != "blocked" || result.NextAction != "manifest_error" {
		t.Fatalf("expected policy block, got %#v", result)
	}
}

func TestDownloadRejectsSizeMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()
	registry := NewRegistry(model.Manifest{Assets: []model.Asset{{
		ID: "a", SizeBytes: 6,
		Sources: []model.Source{{ID: "primary", AssetID: "a", Role: "primary", URL: server.URL, FallbackOrder: 0}},
	}}})
	_, _, err := registry.DownloadAsset(context.Background(), "a", filepath.Join(t.TempDir(), "asset.bin"), model.SourcePolicy{AllowSameFileMirror: true})
	if err == nil || !strings.Contains(err.Error(), "all sources blocked") {
		t.Fatalf("expected size mismatch block, got %v", err)
	}
}

func TestClassifyDeadlineAsTimeout(t *testing.T) {
	if got := classifyError(errors.New("context deadline exceeded")); got != "CONNECT_TIMEOUT" {
		t.Fatalf("expected CONNECT_TIMEOUT, got %s", got)
	}
}
