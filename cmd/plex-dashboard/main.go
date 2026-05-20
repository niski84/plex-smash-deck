package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"plex-dashboard/internal/plexdash"
)

func main() {
	cfg, err := plexdash.LoadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if err := plexdash.EnsureDashboardUI(); err != nil {
		log.Fatalf("dashboard UI: %v", err)
	}

	client := plexdash.NewPlexClient(cfg)
	server := plexdash.NewServer(cfg, client)

	ctx := context.Background()
	go func() {
		server.WarmLibraryCacheOnStartup(ctx)
		server.WarmFanartBannerPrefetch(ctx)
		server.EnrichCollectionIDsInBackground(ctx)
	}()
	go server.StartDailySnapshotWorker(ctx)
	go server.StartConnectivityProbes(ctx)
	go server.WatchSessionsForAudioNorm(ctx)
	server.StartBackgroundWorkers(ctx)

	addr := ":" + cfg.Port
	url := "http://127.0.0.1:" + cfg.Port + "/"
	if wd, err := os.Getwd(); err == nil {
		fmt.Printf("[BOOT] cwd=%s\n", wd)
	}
	fmt.Printf("[BOOT] dashboard UI: %s\n", plexdash.DashboardUISource())
	fmt.Printf("[BOOT] listening on %s — open %s\n", addr, url)

	if !noBrowserRequested() {
		go openBrowserWhenReady(url)
	}

	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
}

// noBrowserRequested returns true when the user has explicitly opted out of
// auto-browser launch via --no-browser flag or PLEX_DASHBOARD_NO_BROWSER=1 env var.
// Also suppressed in headless CI environments.
func noBrowserRequested() bool {
	if os.Getenv("PLEX_DASHBOARD_NO_BROWSER") == "1" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	for _, arg := range os.Args[1:] {
		if arg == "--no-browser" || arg == "-no-browser" {
			return true
		}
	}
	return false
}

// openBrowserWhenReady polls until the server is accepting connections,
// then opens the default system browser.
func openBrowserWhenReady(url string) {
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 30; i++ {
		time.Sleep(300 * time.Millisecond)
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			break
		}
	}
	openBrowser(url)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("[BOOT] browser open failed: %v\n", err)
	}
}
