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

	client := plexdash.NewPlexClient(cfg)
	server := plexdash.NewServer(cfg, client)

	ctx := context.Background()
	go server.WarmLibraryCacheOnStartup(ctx)
	go server.StartDailySnapshotWorker(ctx)

	addr := ":" + cfg.Port
	if wd, err := os.Getwd(); err == nil {
		fmt.Printf("[BOOT] cwd=%s — UI is web/plex-dashboard relative to this directory\n", wd)
	}
	fmt.Printf("[BOOT] plex-dashboard listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, server.Routes()))
}
