package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/index.html web/app.js web/styles.css
var webFiles embed.FS

func webHandler() http.Handler {
	root, _ := fs.Sub(webFiles, "web")
	return http.FileServer(http.FS(root))
}
