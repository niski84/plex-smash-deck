package plexdash

import (
	"context"
	"fmt"
	"time"
)

// StartDailySnapshotWorker fires once a day at midnight UTC, takes a snapshot
// of the Plex library, and discards it if the library hasn't changed since the
// previous snapshot. Call this in a goroutine from main.
func (s *Server) StartDailySnapshotWorker(ctx context.Context) {
	for {
		wait := timeUntilNextMidnightUTC()
		fmt.Printf("[snapshot] next daily snapshot in %s (at %s UTC)\n",
			wait.Round(time.Minute),
			time.Now().UTC().Add(wait).Format("2006-01-02 15:04"))

		select {
		case <-ctx.Done():
			fmt.Println("[snapshot] daily worker stopped")
			return
		case <-time.After(wait):
		}

		s.runDailySnapshot(ctx)
	}
}

func (s *Server) runDailySnapshot(ctx context.Context) {
	cfg := s.snapshot()
	client := NewPlexClient(cfg)

	tctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	fmt.Printf("[snapshot] running daily snapshot (library=%s)...\n", cfg.LibraryKey)
	movies, err := client.ListMovies(tctx, cfg.LibraryKey)
	if err != nil {
		fmt.Printf("[snapshot] daily snapshot failed — list movies: %v\n", err)
		return
	}

	meta, err := TakeSnapshotDedup(movies)
	if err != nil {
		fmt.Printf("[snapshot] daily snapshot failed — save: %v\n", err)
		return
	}
	if meta == nil {
		// Duplicate — already logged inside TakeSnapshotDedup.
		return
	}
	fmt.Printf("[snapshot] daily snapshot saved id=%s count=%d\n", meta.ID, meta.Count)
}

// timeUntilNextMidnightUTC returns the duration until the next UTC midnight.
func timeUntilNextMidnightUTC() time.Duration {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return time.Until(next)
}
