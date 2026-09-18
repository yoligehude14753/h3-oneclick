package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"h3oneclick.local/launcher/internal/model"
)

type Registry struct {
	Assets map[string]model.Asset
	Client *http.Client
}

func NewRegistry(m model.Manifest) *Registry {
	assets := make(map[string]model.Asset, len(m.Assets))
	for _, a := range m.Assets {
		assets[a.ID] = a
	}
	return &Registry{Assets: assets, Client: &http.Client{}}
}

func (r *Registry) Asset(id string) (model.Asset, bool) {
	a, ok := r.Assets[id]
	return a, ok
}

func (r *Registry) Sources(id string, policy model.SourcePolicy) ([]model.Source, error) {
	a, ok := r.Asset(id)
	if !ok {
		return nil, fmt.Errorf("asset %q not found", id)
	}
	sources := append([]model.Source(nil), a.Sources...)
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].FallbackOrder < sources[j].FallbackOrder })
	filtered := make([]model.Source, 0, len(sources))
	for _, s := range sources {
		if s.Role == "same_file_mirror" && !policy.AllowSameFileMirror {
			continue
		}
		if s.Role == "compatible_alternative" && !policy.AllowCompatibleAlternative {
			continue
		}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("asset %q has no source allowed by policy", id)
	}
	return filtered, nil
}

func (r *Registry) Probe(ctx context.Context, id string, policy model.SourcePolicy) model.SourceProbeResult {
	result := model.SourceProbeResult{AssetID: id, MappingStatus: "unverified"}
	sources, err := r.Sources(id, policy)
	if err != nil {
		result.MappingStatus = "blocked"
		result.NextAction = "manifest_error"
		return result
	}
	for i, s := range sources {
		attempt := model.SourceAttempt{AssetID: id, SourceID: s.ID, AttemptNo: i + 1, Probe: "head"}
		started := time.Now()
		status, size, probeErr := r.probeHTTP(ctx, s)
		attempt.HTTPStatus = status
		attempt.ElapsedMS = time.Since(started).Milliseconds()
		if probeErr != nil {
			attempt.ErrorCode = classifyError(probeErr)
			result.Attempts = append(result.Attempts, attempt)
			continue
		}
		if size > 0 && s.SizeBytes > 0 && s.SHA256 != "" && size != s.SizeBytes {
			attempt.ErrorCode = "SIZE_MISMATCH"
			result.Attempts = append(result.Attempts, attempt)
			continue
		}
		attempt.Selected = true
		result.Attempts = append(result.Attempts, attempt)
		result.SelectedSourceRole = s.Role
		if s.Role == "same_file_mirror" {
			result.MappingStatus = "same_repo_path_pending_hash"
		} else if s.Role == "compatible_alternative" {
			result.MappingStatus = "compatible_alternative"
		} else {
			result.MappingStatus = "primary_reachable"
		}
		result.NextAction = "download_selected_source"
		return result
	}
	result.MappingStatus = "blocked"
	result.NextAction = "user_retry_or_change_policy"
	return result
}

func (r *Registry) DownloadAsset(ctx context.Context, id, destination string, policy model.SourcePolicy) ([]model.SourceAttempt, model.Source, error) {
	asset, ok := r.Asset(id)
	if !ok {
		return nil, model.Source{}, fmt.Errorf("asset %q not found", id)
	}
	sources, err := r.Sources(id, policy)
	if err != nil {
		return nil, model.Source{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return nil, model.Source{}, fmt.Errorf("create destination: %w", err)
	}
	var attempts []model.SourceAttempt
	for i, s := range sources {
		attempt := model.SourceAttempt{AssetID: id, SourceID: s.ID, AttemptNo: i + 1, Probe: "download"}
		started := time.Now()
		status, _, probeErr := r.probeHTTP(ctx, s)
		attempt.HTTPStatus = status
		if probeErr != nil {
			attempt.ErrorCode = classifyError(probeErr)
			attempt.ElapsedMS = time.Since(started).Milliseconds()
			attempts = append(attempts, attempt)
			continue
		}
		err := r.download(ctx, s, destination)
		attempt.ElapsedMS = time.Since(started).Milliseconds()
		if err != nil {
			attempt.ErrorCode = classifyError(err)
			attempts = append(attempts, attempt)
			continue
		}
		if err := verifyFile(destination, asset.SizeBytes, s.SHA256); err != nil {
			attempt.ErrorCode = classifyError(err)
			attempts = append(attempts, attempt)
			continue
		}
		attempt.Selected = true
		attempts = append(attempts, attempt)
		return attempts, s, nil
	}
	return attempts, model.Source{}, fmt.Errorf("all sources blocked for asset %q", id)
}

func (r *Registry) probeHTTP(ctx context.Context, s model.Source) (int, int64, error) {
	if s.URL == "" || !strings.HasPrefix(s.URL, "http") {
		return 0, 0, fmt.Errorf("source URL unavailable")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodHead, s.URL, nil)
	if err != nil {
		return 0, 0, err
	}
	response, err := r.Client.Do(request)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented {
		return r.rangeProbe(probeCtx, s.URL)
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return response.StatusCode, response.ContentLength, fmt.Errorf("http status %d", response.StatusCode)
	}
	return response.StatusCode, response.ContentLength, nil
}

func (r *Registry) rangeProbe(ctx context.Context, url string) (int, int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	request.Header.Set("Range", "bytes=0-0")
	response, err := r.Client.Do(request)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 1)
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return response.StatusCode, response.ContentLength, fmt.Errorf("http status %d", response.StatusCode)
	}
	if cr := response.Header.Get("Content-Range"); cr != "" {
		if slash := strings.LastIndex(cr, "/"); slash >= 0 {
			if size, err := strconv.ParseInt(strings.TrimSpace(cr[slash+1:]), 10, 64); err == nil {
				return response.StatusCode, size, nil
			}
		}
	}
	return response.StatusCode, response.ContentLength, nil
}

func (r *Registry) download(ctx context.Context, s model.Source, destination string) error {
	part := destination + ".part"
	var offset int64
	if info, err := os.Stat(part); err == nil {
		offset = info.Size()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := r.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("http status %d", response.StatusCode)
	}
	if offset > 0 && response.StatusCode != http.StatusPartialContent {
		if err := os.Truncate(part, 0); err != nil {
			return err
		}
		offset = 0
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(part, destination)
}

func verifyFile(path string, expectedSize int64, expectedSHA string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return fmt.Errorf("size mismatch: got %d want %d", info.Size(), expectedSize)
	}
	if expectedSHA == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, expectedSHA) {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, expectedSHA)
	}
	return nil
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline"):
		return "CONNECT_TIMEOUT"
	case strings.Contains(message, "size mismatch"):
		return "SIZE_MISMATCH"
	case strings.Contains(message, "sha256"):
		return "HASH_MISMATCH"
	case strings.Contains(message, "http status"):
		return "HTTP_BLOCKED"
	default:
		return "SOURCE_ERROR"
	}
}
