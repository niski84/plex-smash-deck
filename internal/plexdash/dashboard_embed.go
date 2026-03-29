package plexdash

import (
	"fmt"
	"io/fs"
	"net/http"
	"sync"

	appweb "plex-dashboard/web"
)

var (
	dashOnce     sync.Once
	dashHandler  http.Handler
	dashSource   string
	dashInitErr  error
)

// EnsureDashboardUI resolves the dashboard UI from disk (repo layout) or from the copy
// embedded in the binary. Call once before Routes().
func EnsureDashboardUI() error {
	dashOnce.Do(initDashboardRuntime)
	return dashInitErr
}

// DashboardUISource returns a log-friendly path or "embedded web/plex-dashboard".
func DashboardUISource() string {
	dashOnce.Do(initDashboardRuntime)
	return dashSource
}

// DashboardFileServer serves the SPA static files. Call EnsureDashboardUI first (shared init).
func DashboardFileServer() http.Handler {
	dashOnce.Do(initDashboardRuntime)
	return dashHandler
}

func initDashboardRuntime() {
	p, err := resolveDashboardWebRoot()
	if err == nil {
		dashSource = p
		dashHandler = http.FileServer(http.Dir(p))
		return
	}
	sub, err2 := fs.Sub(appweb.PlexDashboard, "plex-dashboard")
	if err2 != nil {
		dashInitErr = fmt.Errorf("cannot find dashboard UI on disk (%v) or in binary (%v)", err, err2)
		return
	}
	dashSource = "embedded web/plex-dashboard"
	dashHandler = http.FileServer(http.FS(sub))
}
