// H3 OneClick desktop GUI: Wails window + embedded deploy engine.
// Same internal packages as the CLI server; API requests fall through the
// asset server to the same handler used by `h3-oneclick -addr`.
package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"h3oneclick.local/launcher/internal/manifest"
	"h3oneclick.local/launcher/internal/server"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	m, err := manifest.Load("")
	if err != nil {
		log.Fatalf("load manifest: %v", err)
	}
	stateDir := defaultStateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		log.Fatalf("create state directory: %v", err)
	}
	srv := server.New(m, stateDir)

	app := NewApp(m.SchemaVersion, stateDir)
	err = wails.Run(&options.App{
		Title:     "H3 OneClick",
		Width:     1400,
		Height:    920,
		MinWidth:  1120,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: srv.Handler(),
		},
		BackgroundColour: &options.RGBA{R: 7, G: 11, B: 7, A: 255},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About:    &mac.AboutInfo{Title: "H3 OneClick", Message: "MiniMax-H3 one-click deployment v" + m.SchemaVersion},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func defaultStateDir() string {
	if value := os.Getenv("H3_ONECLICK_STATE_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".h3-oneclick")
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Local", "H3OneClick")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "H3OneClick")
	default:
		return filepath.Join(home, ".local", "share", "h3-oneclick")
	}
}
