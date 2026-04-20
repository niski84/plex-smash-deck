package plexdash

import (
	"io/fs"
	"net/http"
	"os"

	plexdashboardnext "plex-dashboard/web/plex-dashboard-next"
)

// registerBetaRoutes wires up the new UI at /beta.
// Called from Routes() after all API routes are registered.
func (s *Server) registerBetaRoutes(mux *http.ServeMux) {
	// Static assets for the new UI.
	nextStatic, _ := fs.Sub(plexdashboardnext.FS, "static")
	mux.Handle("/next/static/", http.StripPrefix("/next/static/", http.FileServer(http.FS(nextStatic))))

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
