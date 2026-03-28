// Command lg-ssap-probe runs experimental LG webOS SSAP queries (foreground app, audio, etc.).
// Requires LGTV_ADDR and LGTV_CLIENT_KEY (e.g. from .env in the project root).
//
//	go run ./cmd/lg-ssap-probe
package main

import (
	"context"
	"fmt"
	"os"

	"plex-dashboard/internal/plexdash"
)

func main() {
	cfg, err := plexdash.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if cfg.LGTVAddr == "" || cfg.LGTVClientKey == "" {
		fmt.Fprintf(os.Stderr, "Set LGTV_ADDR and LGTV_CLIENT_KEY (paired TV IP and client key).\n")
		os.Exit(1)
	}

	fmt.Printf("Probing SSAP at %s …\n", cfg.LGTVAddr)
	fmt.Println("Uses extended manifest permissions (LGWebOSRemote-style). If everything was 401, the client key was likely paired with a minimal manifest — re-pair after expanding permissions in PairLGTV.")
	fmt.Println()
	results := plexdash.SSAPProbeExperiments(context.Background(), cfg.LGTVAddr, cfg.LGTVClientKey)
	for _, r := range results {
		fmt.Println(r.String())
		fmt.Println()
	}
}
