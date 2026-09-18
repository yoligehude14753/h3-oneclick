package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"h3oneclick.local/launcher/internal/manifest"
	"h3oneclick.local/launcher/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "local web address")
	manifestPath := flag.String("manifest", "", "optional external manifest JSON")
	stateDir := flag.String("state-dir", defaultStateDir(), "state and receipt directory")
	flag.Parse()

	m, err := manifest.Load(*manifestPath)
	if err != nil {
		log.Fatalf("load manifest: %v", err)
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		log.Fatalf("create state directory: %v", err)
	}
	app := server.New(m, *stateDir)
	url := "http://" + *addr
	fmt.Printf("H3 OneClick Launcher %s\n", m.SchemaVersion)
	fmt.Printf("Local UI: %s\n", url)
	log.Fatal(http.ListenAndServe(*addr, app.Handler()))
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
