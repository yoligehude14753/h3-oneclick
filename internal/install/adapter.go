package install

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed adapter/__init__.py adapter/web/h3-oneclick.js
var templateAdapterFS embed.FS

func installTemplateAdapter(comfyRoot string) error {
	if comfyRoot == "" {
		return fmt.Errorf("ComfyUI root is empty")
	}
	targetRoot := filepath.Join(comfyRoot, "custom_nodes", "H3OneClickAdapter")
	files := []struct {
		source string
		target string
	}{
		{"adapter/__init__.py", filepath.Join(targetRoot, "__init__.py")},
		{"adapter/web/h3-oneclick.js", filepath.Join(targetRoot, "web", "h3-oneclick.js")},
	}
	for _, file := range files {
		data, err := fs.ReadFile(templateAdapterFS, file.source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(file.target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file.target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
