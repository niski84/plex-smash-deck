package plexdash

import (
	"io/fs"
	"net/http"
	"os"

	plexdashboardnext "plex-dashboard/web/plex-dashboard-next"
)

func noCacheHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// diskOrEmbedded returns a handler that serves from diskRoot when the file exists there,
// falling back to the embedded fallback handler. This lets JS/CSS edits take effect
// without a binary rebuild when running in dev (repo layout on disk).
func diskOrEmbedded(diskRoot http.Dir, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(string(diskRoot) + "/" + r.URL.Path)
		if err == nil {
			f.Close()
			http.FileServer(diskRoot).ServeHTTP(w, r)
			return
		}
		fallback.ServeHTTP(w, r)
	})
}

// registerBetaRoutes wires up the new UI at /beta.
// Called from Routes() after all API routes are registered.
func (s *Server) registerBetaRoutes(mux *http.ServeMux) {
	// Static assets — prefer disk so JS/CSS edits take effect without a rebuild.
	// Always no-store so browsers re-fetch on every deploy.
	nextStatic, _ := fs.Sub(plexdashboardnext.FS, "static")
	diskStatic := http.Dir("web/plex-dashboard-next/static")
	mux.Handle("/next/static/", http.StripPrefix("/next/static/", noCacheHandler(diskOrEmbedded(diskStatic, http.FileServer(http.FS(nextStatic))))))

	// New dashboard at /beta.
	mux.HandleFunc("/beta", s.handleNewDashboard)
}

func (s *Server) handleNewDashboard(w http.ResponseWriter, r *http.Request) {
	// Only serve the SPA index at exact "/beta".
	if r.URL.Path != "/beta" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Prefer disk so edits are hot-reloaded without a binary rebuild.
	if data, err := os.ReadFile("web/plex-dashboard-next/index.html"); err == nil {
		_, _ = w.Write(data)
		return
	}

	// Fall back to binary-embedded copy.
	data, err := plexdashboardnext.FS.ReadFile("index.html")
	if err != nil {
		http.Error(w, "dashboard not found", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}
