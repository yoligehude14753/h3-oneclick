package manifest

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"

	"h3oneclick.local/launcher/internal/model"
)

//go:embed default.json
var embedded embed.FS

func Load(path string) (model.Manifest, error) {
	var data []byte
	var err error
	if path != "" {
		data, err = os.ReadFile(path)
	} else {
		data, err = embedded.ReadFile("default.json")
	}
	if err != nil {
		return model.Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m model.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return model.Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if m.SchemaVersion == "" || len(m.Profiles) == 0 {
		return model.Manifest{}, fmt.Errorf("manifest is missing schema_version or profiles")
	}
	return m, nil
}

func AssetMap(m model.Manifest) map[string]model.Asset {
	out := make(map[string]model.Asset, len(m.Assets))
	for _, a := range m.Assets {
		out[a.ID] = a
	}
	return out
}

func ProfileMap(m model.Manifest) map[string]model.Profile {
	out := make(map[string]model.Profile, len(m.Profiles))
	for _, p := range m.Profiles {
		out[p.ID] = p
	}
	return out
}
