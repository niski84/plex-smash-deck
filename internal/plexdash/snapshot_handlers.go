package plexdash

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// handleSnapshots: GET = list snapshots, POST = take a new one.
func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		list, err := ListSnapshots()
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"snapshots": list}})

	case http.MethodPost:
		cfg := s.snapshot()
		client := NewPlexClient(cfg)
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		movies, err := client.ListMovies(ctx, cfg.LibraryKey)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Error: "failed to fetch movies: " + err.Error()})
			return
		}
		snap, err := TakeSnapshot(movies)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Error: "failed to save snapshot: " + err.Error()})
			return
		}
		// Return meta only (no full movie list) to keep response small.
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
			"snapshot": SnapshotMeta{ID: snap.ID, CapturedAt: snap.CapturedAt, Count: snap.Count},
		}})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSnapshotByID: GET /api/snapshots/{id}
func (s *Server) handleSnapshotByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/snapshots/")
	if id == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Error: "snapshot ID required"})
		return
	}
	snap, err := LoadSnapshot(id)
	if err != nil {
		respondJSON(w, http.StatusNotFound, apiResponse{Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"snapshot": snap}})
}

// handleSnapshotDiff: GET /api/snapshots/diff?from=ID&to=ID
func (s *Server) handleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fromID := r.URL.Query().Get("from")
	toID := r.URL.Query().Get("to")
	if fromID == "" || toID == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Error: "'from' and 'to' query params are required"})
		return
	}
	diff, err := DiffSnapshots(fromID, toID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"diff": diff}})
}

// handleSnapshotLatestDiff: GET /api/snapshots/latest-diff
// Returns the diff between the two most recent snapshots.
func (s *Server) handleSnapshotLatestDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	diff, err := LatestDiff()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if diff == nil {
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"diff": nil, "message": "fewer than 2 snapshots exist"}})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"diff": diff}})
}

// handleSnapshotMissing: GET /api/snapshots/missing
// Scans all consecutive snapshot pairs and returns movies that went missing.
func (s *Server) handleSnapshotMissing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	events, err := FindAllMissing()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"missing": events,
		"count":   len(events),
	}})
}
