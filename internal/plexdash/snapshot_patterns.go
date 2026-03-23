package plexdash

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// PatternInsight describes a recurring trait shared across multiple newly added movies.
type PatternInsight struct {
	Kind   string   `json:"kind"`   // "director" | "actor" | "genre" | "decade"
	Name   string   `json:"name"`   // e.g. "John Carpenter", "Horror", "1980s"
	Count  int      `json:"count"`  // how many of the added movies share this trait
	Total  int      `json:"total"`  // total movies in the added set
	Movies []string `json:"movies"` // titles that carry this trait
}

// PatternsResult is returned by /api/snapshots/patterns.
type PatternsResult struct {
	TotalAdded int              `json:"totalAdded"`
	Insights   []PatternInsight `json:"insights"`
}

// analyzeAddedMovies computes recurring patterns across the added movie set.
// plexMovies is the live library — used to fill in actors for snapshots that
// pre-date the Actors field being stored in SnapshotMovie.
func analyzeAddedMovies(added []SnapshotMovie, plexMovies []Movie) PatternsResult {
	if len(added) == 0 {
		return PatternsResult{TotalAdded: 0, Insights: []PatternInsight{}}
	}

	// Build title+year key → actors from the live Plex library (fallback for old snapshots).
	liveActors := make(map[string][]string, len(plexMovies))
	for _, m := range plexMovies {
		key := snapshotKey(SnapshotMovie{Title: m.Title, Year: m.Year})
		liveActors[key] = m.Actors
	}

	type bucket struct {
		kind   string
		name   string
		movies []string
	}
	buckets := map[string]*bucket{}

	touch := func(kind, name, title string) {
		k := kind + ":" + name
		if buckets[k] == nil {
			buckets[k] = &bucket{kind: kind, name: name}
		}
		buckets[k].movies = append(buckets[k].movies, title)
	}

	for _, m := range added {
		for _, d := range m.Directors {
			touch("director", d, m.Title)
		}
		for _, g := range m.Genres {
			touch("genre", g, m.Title)
		}

		// Prefer actors stored in the snapshot; fall back to live library.
		actors := m.Actors
		if len(actors) == 0 {
			actors = liveActors[snapshotKey(m)]
		}
		// Cap actors at top 5 billed to avoid noise from deep cast lists.
		for i, a := range actors {
			if i >= 5 {
				break
			}
			touch("actor", a, m.Title)
		}

		if m.Year > 0 {
			decade := (m.Year / 10) * 10
			touch("decade", fmt.Sprintf("%ds", decade), m.Title)
		}
	}

	kindOrder := map[string]int{"director": 0, "actor": 1, "genre": 2, "decade": 3}

	insights := make([]PatternInsight, 0, len(buckets))
	for _, b := range buckets {
		if len(b.movies) < 2 {
			continue
		}
		// Deduplicate movie titles within a bucket (a movie may share genre with itself).
		seen := map[string]struct{}{}
		deduped := b.movies[:0]
		for _, t := range b.movies {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				deduped = append(deduped, t)
			}
		}
		if len(deduped) < 2 {
			continue
		}
		insights = append(insights, PatternInsight{
			Kind:   b.kind,
			Name:   b.name,
			Count:  len(deduped),
			Total:  len(added),
			Movies: deduped,
		})
	}

	sort.Slice(insights, func(i, j int) bool {
		ci, cj := insights[i].Count, insights[j].Count
		if ci != cj {
			return ci > cj
		}
		return kindOrder[insights[i].Kind] < kindOrder[insights[j].Kind]
	})

	// Keep the most interesting ones: top director/actor hits + genre summary + decade.
	const maxInsights = 8
	if len(insights) > maxInsights {
		insights = insights[:maxInsights]
	}

	return PatternsResult{TotalAdded: len(added), Insights: insights}
}

// handleSnapshotPatterns: GET /api/snapshots/patterns?from=ID&to=ID
// from/to are optional; without them the latest diff is used.
func (s *Server) handleSnapshotPatterns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	fromID := r.URL.Query().Get("from")
	toID := r.URL.Query().Get("to")

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var added []SnapshotMovie

	if fromID != "" && toID != "" {
		diff, err := DiffSnapshots(fromID, toID)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
			return
		}
		added = diff.Added
	} else {
		diff, err := LatestDiff()
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
			return
		}
		if diff != nil {
			added = diff.Added
		}
	}

	// Fetch live library to backfill actors for older snapshots.
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	liveMovies, err := client.ListMovies(ctx, cfg.LibraryKey)
	if err != nil {
		// Non-fatal — patterns will be computed without actor data.
		fmt.Printf("[patterns] could not fetch live library: %v\n", err)
		liveMovies = nil
	}

	result := analyzeAddedMovies(added, liveMovies)
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"patterns": result}})
}
