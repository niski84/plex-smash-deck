package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

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
	}()
	go server.StartDailySnapshotWorker(ctx)
	go server.StartConnectivityProbes(ctx)

	addr := ":" + cfg.Port
	if wd, err := os.Getwd(); err == nil {
		fmt.Printf("[BOOT] cwd=%s\n", wd)
	}
	fmt.Printf("[BOOT] dashboard UI: %s\n", plexdash.DashboardUISource())
	fmt.Printf("[BOOT] listening on %s — open http://127.0.0.1:%s/\n", addr, cfg.Port)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
}
