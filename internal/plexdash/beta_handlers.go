package plexdash

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"time"

	plexdashboardnext "plex-dashboard/web/plex-dashboard-next"

	"plex-dashboard/internal/plexdash/views/components"
)

// registerBetaRoutes wires up the /beta/* routes onto the given mux.
// Called from Routes() after all existing routes are registered.
func (s *Server) registerBetaRoutes(mux *http.ServeMux) {
	// Static assets for the new UI.
	nextStatic, _ := fs.Sub(plexdashboardnext.FS, "static")
	mux.Handle("/next/static/", http.StripPrefix("/next/static/", http.FileServer(http.FS(nextStatic))))

	// Full-page SPA.
	mux.HandleFunc("/beta", s.handleBeta)
}

func (s *Server) handleBeta(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/beta" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Prefer disk so edits are visible without a rebuild (dev hot-reload).
	if data, err := os.ReadFile("web/plex-dashboard-next/index.html"); err == nil {
		_, _ = w.Write(data)
		return
	}

	// Fall back to embedded binary.
	data, err := plexdashboardnext.FS.ReadFile("index.html")
	if err != nil {
		http.Error(w, "dashboard not found", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}

// buildPlaybackVM fetches the current playback state and builds the view model.
func (s *Server) buildPlaybackVM(ctx context.Context) components.PlaybackVM {
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	sessions, sessionsErr := client.ListPlaybackSessions(ctx)
	if sessionsErr != nil {
		sessions = nil
	}

	s.playbackMu.RLock()
	local := s.localPlayback
	s.playbackMu.RUnlock()

	payload := buildPlaybackStatusPayload(cfg, sessions, sessionsErr, local)

	vm := components.PlaybackVM{
		Target:      cfg.TargetClientName,
		PrimaryFrom: "idle",
		SummaryLine: "No active Plex session.",
	}

	if pf, ok := payload["primaryFrom"].(string); ok {
		vm.PrimaryFrom = pf
	}
	if sl, ok := payload["summaryLine"].(string); ok {
		vm.SummaryLine = sl
	}

	switch vm.PrimaryFrom {
	case "plex_session":
		if sess, ok := payload["primary"].(PlaybackSession); ok {
			vm.SessionTitle = sess.DisplayTitle()
			vm.SessionPlayer = sess.PlayerName
			vm.SessionState = sess.PlayerState
			vm.ProgressPercent = sess.ProgressPercent
		}
	case "local_send":
		vm.Titles = local.Titles
		vm.Stale = !local.SentAt.IsZero() && time.Since(local.SentAt) > localPlaybackFreshness
	}

	return vm
}
