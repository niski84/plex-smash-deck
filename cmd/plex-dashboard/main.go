package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

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
	go server.StartDailySnapshotWorker(ctx)

	addr := ":" + cfg.Port
	fmt.Printf("[BOOT] plex-dashboard listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, server.Routes()))
}
