package plexdash

import (
	"context"
	"fmt"
	"time"
)

// StartDailySnapshotWorker fires once per day at the configured hour (UTC),
// takes a snapshot, and discards it if the library count hasn't changed since
// the previous snapshot. The schedule is re-read on every loop tick so changes
// made in the Settings UI take effect without restarting the server.
// Call this in a goroutine from main.
func (s *Server) StartDailySnapshotWorker(ctx context.Context) {
	for {
		cfg := s.snapshot()

		if cfg.SnapshotDisabled {
			fmt.Println("[snapshot] daily snapshots disabled — checking again in 1h")
			select {
			case <-ctx.Done():
				fmt.Println("[snapshot] daily worker stopped")
				return
			case <-time.After(time.Hour):
			}
			continue
		}

		wait := timeUntilNextScheduledHourUTC(cfg.SnapshotHour)
		fmt.Printf("[snapshot] next daily snapshot in %s (at %02d:00 UTC on %s)\n",
			wait.Round(time.Minute),
			cfg.SnapshotHour,
			time.Now().UTC().Add(wait).Format("2006-01-02"))

		select {
		case <-ctx.Done():
			fmt.Println("[snapshot] daily worker stopped")
			return
		case <-time.After(wait):
		}

		// Re-read config once more at fire time — the user may have changed the
		// hour or disabled snapshots while we were waiting.
		cfg = s.snapshot()
		if cfg.SnapshotDisabled {
			fmt.Println("[snapshot] snapshot disabled at fire time — skipping")
			continue
		}

		s.runDailySnapshot(ctx)
	}
}

func (s *Server) runDailySnapshot(ctx context.Context) {
	cfg := s.snapshot()
	fmt.Printf("[snapshot] running daily snapshot (library=%s)...\n", cfg.LibraryKey)

	tctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Always fetch fresh data for a snapshot — invalidate the shared cache first.
	s.invalidateMovieListCache()
	movies, err := s.cachedListMovies(tctx)
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
		fmt.Println("[snapshot] no change since last snapshot — skipped")
		return
	}
	fmt.Printf("[snapshot] daily snapshot saved id=%s count=%d\n", meta.ID, meta.Count)
}

// timeUntilNextScheduledHourUTC returns the duration until the next occurrence
// of the given UTC hour (0-23). If that hour has already passed today it
// returns a duration pointing to tomorrow at that hour.
func timeUntilNextScheduledHourUTC(hour int) time.Duration {
	if hour < 0 || hour > 23 {
		hour = 2 // safety fallback
	}
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return time.Until(next)
}
