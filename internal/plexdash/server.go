package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	mu           sync.RWMutex
	cfg          Config
	settingsPath string
}

const (
	minAllowedYear = 1982
	maxAllowedYear = 2016
)

func NewServer(cfg Config, client *PlexClient) *Server {
	_ = client
	return &Server{
		cfg:          cfg,
		settingsPath: defaultSettingsPath(),
	}
}

func (s *Server) snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) replaceConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/movies", s.handleMovies)
	mux.HandleFunc("/api/genres", s.handleGenres)
	mux.HandleFunc("/api/players", s.handlePlayers)
	mux.HandleFunc("/api/discovery/person-suggest", s.handleDiscoveryPersonSuggest)
	mux.HandleFunc("/api/discovery/collaborators", s.handleDiscoveryCollaborators)
	mux.HandleFunc("/api/discovery/filmography", s.handleDiscoveryFilmography)
	mux.HandleFunc("/api/discovery/poster", s.handleDiscoveryPoster)
	mux.HandleFunc("/api/discovery/radarr/add", s.handleDiscoveryAddToRadarr)
	mux.HandleFunc("/api/snapshots/latest-diff", s.handleSnapshotLatestDiff)
	mux.HandleFunc("/api/snapshots/missing", s.handleSnapshotMissing)
	mux.HandleFunc("/api/snapshots/diff", s.handleSnapshotDiff)
	mux.HandleFunc("/api/snapshots/patterns", s.handleSnapshotPatterns)
	mux.HandleFunc("/api/snapshots/", s.handleSnapshotByID)
	mux.HandleFunc("/api/snapshots", s.handleSnapshots)

	mux.HandleFunc("/api/playlists/preview", s.handlePreviewPlaylist)
	mux.HandleFunc("/api/playlists", s.handleListPlaylists)
	mux.HandleFunc("/api/playlists/items", s.handlePlaylistItems)
	mux.HandleFunc("/api/playlists/play", s.handlePlayPlaylist)
	mux.HandleFunc("/api/playlists/random", s.handleCreateRandomPlaylist)
	mux.HandleFunc("/api/playlists/by-people", s.handleCreatePlaylistByPeople)
	mux.HandleFunc("/api/playlists/by-genre-rating", s.handleCreatePlaylistByGenreRating)
	mux.HandleFunc("/api/playlists/random-play", s.handleCreateAndPlayRandomPlaylist)

	// Serve static dashboard UI.
	mux.Handle("/", http.FileServer(http.Dir(filepath.Clean("web/plex-dashboard"))))

	return requestLogMiddleware(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"service": "plex-dashboard",
		},
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.snapshot()
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: cfg})
		return
	case http.MethodPost:
		var req Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
			return
		}
		req.PlexBaseURL = strings.TrimRight(req.PlexBaseURL, "/")
		req.RadarrURL = strings.TrimRight(req.RadarrURL, "/")
		if req.Port == "" {
			req.Port = s.snapshot().Port
		}
		if req.LibraryKey == "" {
			req.LibraryKey = "1"
		}
		if req.TargetClientName == "" {
			req.TargetClientName = "Living Room"
		}
		if req.RadarrProfileID <= 0 {
			req.RadarrProfileID = 1
		}

		// Persist settings and refresh in-memory configuration.
		if err := SavePersistedConfig(s.settingsPath, req); err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
			return
		}
		s.replaceConfig(req)
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"saved": true}})
		return
	default:
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
	}
}

func (s *Server) handleMovies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/movies libraryKey=%s\n", cfg.LibraryKey)
	movies, err := client.ListMovies(ctx, cfg.LibraryKey)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	limit := 100
	actor := r.URL.Query().Get("actor")
	director := r.URL.Query().Get("director")
	genre := r.URL.Query().Get("genre")
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	if actor != "" || director != "" {
		movies = filterMoviesByPeople(movies, actor, director)
	}
	if genre != "" {
		movies = filterMoviesByGenreRatingYear(movies, genre, 0, 0, 9999)
	}
	if limit > len(movies) {
		limit = len(movies)
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"count":  len(movies),
			"movies": movies[:limit],
		},
	})
}

func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	genres, err := client.ListGenres(ctx, cfg.LibraryKey)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"genres": genres,
		},
	})
}

func (s *Server) handleDiscoveryFilmography(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	person := strings.TrimSpace(r.URL.Query().Get("person"))
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	playlistTitle := strings.TrimSpace(r.URL.Query().Get("playlistTitle"))
	director := strings.TrimSpace(r.URL.Query().Get("director"))
	coActor := strings.TrimSpace(r.URL.Query().Get("coActor"))
	minYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("minYear")))
	maxYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("maxYear")))
	minRating, _ := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("minRating")), 64)
	if person == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "person query is required"})
		return
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	var cacheStats DiscoveryCacheStats
	items, err := AnalyzeFilmography(ctx, cfg, client, person, role, playlistTitle, director, coActor, minYear, maxYear, minRating, &cacheStats)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	missing := 0
	nPosterURL := 0
	nPosterPath := 0
	for _, item := range items {
		if !item.InLibrary {
			missing++
		}
		if strings.TrimSpace(item.PosterURL) != "" {
			nPosterURL++
		}
		if strings.TrimSpace(item.PosterPath) != "" {
			nPosterPath++
		}
	}
	DiscoveryDebugf("[filmography] person=%q role=%q total=%d missing_lib=%d posterUrl_nonempty=%d posterPath_nonempty=%d cache=%+v",
		person, role, len(items), missing, nPosterURL, nPosterPath, cacheStats)
	if len(items) > 0 {
		s := items[0]
		DiscoveryDebugf("[filmography] sample tmdbId=%d title=%q posterUrl_len=%d posterPath=%q",
			s.TMDBID, s.Title, len(strings.TrimSpace(s.PosterURL)), strings.TrimSpace(s.PosterPath))
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"person":        person,
			"role":          role,
			"playlistTitle": playlistTitle,
			"director":      director,
			"coActor":       coActor,
			"minYear":       minYear,
			"maxYear":       maxYear,
			"minRating":     minRating,
			"total":         len(items),
			"missing":       missing,
			"items":         items,
			"cache":         cacheStats,
		},
	})
}

func (s *Server) handleDiscoveryPersonSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	suggestions, err := SuggestPeople(ctx, cfg, query)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	movies, _ := client.ListMovies(ctx, cfg.LibraryKey)
	role := DiscoverCollaborators(movies, query).SuggestedRole
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"suggestions": suggestions, "suggestedRole": role}})
}

func (s *Server) handleDiscoveryCollaborators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	person := strings.TrimSpace(r.URL.Query().Get("person"))
	if person == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "person query is required"})
		return
	}
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	movies, err := client.ListMovies(ctx, cfg.LibraryKey)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	collabs := DiscoverCollaborators(movies, person)
	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"suggestedRole": collabs.SuggestedRole,
			"directors":     collabs.Directors,
			"actors":        collabs.Actors,
		},
	})
}

func (s *Server) handleDiscoveryAddToRadarr(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	var req struct {
		Items []RadarrAddItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	if len(req.Items) == 0 {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "no selected items provided"})
		return
	}

	cfg := s.snapshot()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := AddMoviesToRadarr(ctx, cfg, req.Items)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: result})
}

func (s *Server) handleCreateRandomPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Title string `json:"title"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}
	if req.Title == "" {
		req.Title = "Tonight Picks"
	}
	if req.Count <= 0 {
		req.Count = 12
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/random title=%q count=%d\n", req.Title, req.Count)
	result, err := client.CreateRandomPlaylist(ctx, cfg.LibraryKey, req.Title, req.Count)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    result,
	})
}

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	playlists, err := client.ListPlaylists(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"playlists": playlists}})
}

func (s *Server) handlePlaylistItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	if title == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "title query is required"})
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if limit <= 0 {
		limit = 300
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	movies, err := client.PlaylistMovies(ctx, title, limit)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"title": title, "count": len(movies), "movies": movies}})
}

func (s *Server) handlePlayPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	var req struct {
		Title      string `json:"title"`
		ClientName string `json:"clientName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "playlist title is required"})
		return
	}
	if strings.TrimSpace(req.ClientName) == "" {
		req.ClientName = s.snapshot().TargetClientName
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, err := client.PlayPlaylistOnClient(ctx, req.Title, req.ClientName)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: result})
}

func (s *Server) handlePreviewPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	var req struct {
		Title     string  `json:"title"`
		Genre     string  `json:"genre"`
		MinRating float64 `json:"minRating"`
		Actor     string  `json:"actor"`
		Director  string  `json:"director"`
		MinYear   int     `json:"minYear"`
		MaxYear   int     `json:"maxYear"`
		Limit     int     `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	if req.MinYear == 0 {
		req.MinYear = minAllowedYear
	}
	if req.MaxYear == 0 {
		req.MaxYear = maxAllowedYear
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	movies, err := client.ListMovies(ctx, cfg.LibraryKey)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	if strings.TrimSpace(req.Actor) != "" || strings.TrimSpace(req.Director) != "" {
		movies = filterMoviesByPeople(movies, req.Actor, req.Director)
	}
	movies = filterMoviesByGenreRatingYear(movies, req.Genre, req.MinRating, req.MinYear, req.MaxYear)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	movies = ReorderMoviesByViewCountShuffleTies(movies, rng)

	total := len(movies)
	if req.Limit > len(movies) {
		req.Limit = len(movies)
	}
	preview := movies[:req.Limit]

	title := strings.TrimSpace(req.Title)
	if title == "" {
		if strings.TrimSpace(req.Genre) == "" {
			title = "ALL-rando"
		} else {
			title = strings.ToUpper(strings.TrimSpace(req.Genre)) + "-rando"
		}
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"suggestedTitle": title,
			"totalMatched":   total,
			"showing":        len(preview),
			"filters": map[string]any{
				"genre":     req.Genre,
				"minRating": req.MinRating,
				"actor":     req.Actor,
				"director":  req.Director,
				"minYear":   req.MinYear,
				"maxYear":   req.MaxYear,
			},
			"movies": preview,
		},
	})
}

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/players targetClient=%q\n", cfg.TargetClientName)
	players, err := client.ListPlayers(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"targetClient": cfg.TargetClientName,
			"players":      players,
		},
	})
}

func (s *Server) handleCreatePlaylistByPeople(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Title    string `json:"title"`
		Count    int    `json:"count"`
		Actor    string `json:"actor"`
		Director string `json:"director"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}
	if req.Title == "" {
		req.Title = "People Picks"
	}
	if req.Count <= 0 {
		req.Count = 12
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/by-people title=%q count=%d actor=%q director=%q\n", req.Title, req.Count, req.Actor, req.Director)
	result, err := client.CreatePlaylistByPeople(ctx, cfg.LibraryKey, req.Title, req.Actor, req.Director, req.Count)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    result,
	})
}

func (s *Server) handleCreateAndPlayRandomPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Title      string `json:"title"`
		Count      int    `json:"count"`
		ClientName string `json:"clientName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}
	if req.Title == "" {
		req.Title = "Tonight Picks"
	}
	if req.Count <= 0 {
		req.Count = 12
	}
	if req.ClientName == "" {
		req.ClientName = s.snapshot().TargetClientName
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/random-play title=%q count=%d client=%q\n", req.Title, req.Count, req.ClientName)
	result, err := client.CreateRandomPlaylistAndPlay(ctx, cfg.LibraryKey, req.Title, req.Count, req.ClientName)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    result,
	})
}

func (s *Server) handleCreatePlaylistByGenreRating(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Genre     string  `json:"genre"`
		MinRating float64 `json:"minRating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/by-genre-rating genre=%q minRating=%.1f years=%d-%d\n", req.Genre, req.MinRating, minAllowedYear, maxAllowedYear)
	result, err := client.CreatePlaylistByGenreRatingYear(ctx, cfg.LibraryKey, req.Genre, req.MinRating, minAllowedYear, maxAllowedYear)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"playlist": result,
			"rule": map[string]any{
				"minYear":   minAllowedYear,
				"maxYear":   maxAllowedYear,
				"minRating": req.MinRating,
				"genre":     req.Genre,
			},
		},
	})
}

type apiResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, payload apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[HTTP] %s %s\n", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
