package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// StaticHandler returns an http.Handler that serves the embedded SPA frontend.
// It serves static files from the embedded dist directory and falls back to
// index.html for SPA routing (e.g., /setup, /login, /app/dashboard).
func StaticHandler() http.Handler {
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		return noBuildHandler()
	}

	// Verify that index.html exists (frontend has been built)
	_, err = subFS.Open("index.html")
	if err != nil {
		return noBuildHandler()
	}

	fileServer := http.FileServer(http.FS(subFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path and remove leading slash
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "."
		}

		// Try to open the requested file
		f, err := subFS.Open(p)
		if err != nil {
			// File not found: SPA fallback to index.html
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()

		// File exists, serve it
		fileServer.ServeHTTP(w, r)
	})
}

func noBuildHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Frontend not built. Run scripts/build.ps1 first.\n"))
	})
}
