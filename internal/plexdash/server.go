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

// movieListCacheTTL controls how long the in-memory movie list is reused before
// the next request triggers a fresh Plex fetch. All handlers (discovery, snapshots,
// playlist builders, etc.) share this one copy, so Plex is only hit once per window.
const movieListCacheTTL = 15 * time.Minute

type Server struct {
	mu           sync.RWMutex
	cfg          Config
	settingsPath string

	mlMu       sync.RWMutex
	mlMovies   []Movie
	mlCachedAt time.Time
	mlKey      string // library key the cache was built for

	discJobs *discoveryJobStore
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
		discJobs:     newDiscoveryJobStore(),
	}
}

// cachedListMovies returns the in-memory movie list if it is still fresh,
// otherwise fetches from Plex and updates the cache.
func (s *Server) cachedListMovies(ctx context.Context) ([]Movie, error) {
	cfg := s.snapshot()
	s.mlMu.RLock()
	if s.mlKey == cfg.LibraryKey && len(s.mlMovies) > 0 && time.Since(s.mlCachedAt) < movieListCacheTTL {
		movies := s.mlMovies
		s.mlMu.RUnlock()
		return movies, nil
	}
	s.mlMu.RUnlock()

	client := NewPlexClient(cfg)
	movies, err := client.ListMovies(ctx, cfg.LibraryKey)
	if err != nil {
		return nil, err
	}

	s.mlMu.Lock()
	s.mlMovies = movies
	s.mlCachedAt = time.Now()
	s.mlKey = cfg.LibraryKey
	s.mlMu.Unlock()

	return movies, nil
}

// invalidateMovieListCache forces the next cachedListMovies call to fetch fresh data.
func (s *Server) invalidateMovieListCache() {
	s.mlMu.Lock()
	s.mlMovies = nil
	s.mlMu.Unlock()
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
	mux.HandleFunc("/api/discovery/studio", s.handleDiscoveryStudio)
	mux.HandleFunc("/api/discovery/start", s.handleDiscoveryStart)
	mux.HandleFunc("/api/discovery/poll", s.handleDiscoveryPoll)
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
	mux.HandleFunc("/api/plex/thumb", s.handlePlexThumb)
	mux.HandleFunc("/api/movies/play", s.handleMoviesPlay)
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
		if req.SnapshotHour < 0 || req.SnapshotHour > 23 {
			req.SnapshotHour = 2
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

	// ?nocache=1 lets the "Refresh Movies" button bypass and repopulate the cache.
	if r.URL.Query().Get("nocache") == "1" {
		s.invalidateMovieListCache()
	}

	cfg := s.snapshot()
	fmt.Printf("[API] /api/movies libraryKey=%s cached=%v\n", cfg.LibraryKey, r.URL.Query().Get("nocache") != "1")
	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	limit := len(movies) // default: return all
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

// handlePlexThumb proxies a Plex poster thumbnail by ratingKey, keeping the
// Plex token server-side. Images are cached permanently on disk so Plex is
// only hit once per movie poster; the browser also caches for 1 year.
func (s *Server) handlePlexThumb(w http.ResponseWriter, r *http.Request) {
	ratingKey := r.URL.Query().Get("ratingKey")
	if ratingKey == "" {
		http.NotFound(w, r)
		return
	}
	// Sanitise ratingKey — must be numeric-ish to form a safe filename.
	for _, c := range ratingKey {
		if (c < '0' || c > '9') && c != '-' {
			http.Error(w, "invalid ratingKey", http.StatusBadRequest)
			return
		}
	}
	cfg := s.snapshot()
	thumbURL := cfg.PlexBaseURL + "/library/metadata/" + ratingKey + "/thumb?X-Plex-Token=" + cfg.PlexToken
	cachePath := plexThumbCacheDir + "/" + ratingKey + ".jpg"

	if !serveOrCachePoster(w, thumbURL, cachePath, "image/jpeg") {
		http.NotFound(w, r)
	}
}

// handleMoviesPlay streams a caller-supplied list of Plex movies to the TV via
// the webOS native media player. The caller provides ratingKey + partKey so we
// never need a second round-trip to Plex just to locate the file URL.
func (s *Server) handleMoviesPlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	var req struct {
		Items []struct {
			RatingKey string `json:"ratingKey"`
			PartKey   string `json:"partKey"`
			Container string `json:"container"`
			Title     string `json:"title"`
			PartSize  int64  `json:"partSize"`
		} `json:"items"`
		ClientName string `json:"clientName"`
		Shuffle    bool   `json:"shuffle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON: " + err.Error()})
		return
	}
	if len(req.Items) == 0 {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "items array is empty"})
		return
	}

	cfg := s.snapshot()
	streamItems := make([]WebOSStreamItem, 0, len(req.Items))
	for _, item := range req.Items {
		mimeContainer := item.Container
		if mimeContainer == "" {
			mimeContainer = "mp4"
		}
		streamItems = append(streamItems, WebOSStreamItem{
			StreamURL: cfg.PlexBaseURL + item.PartKey + "?X-Plex-Token=" + cfg.PlexToken,
			Title:     item.Title,
			Container: mimeContainer,
			Size:      item.PartSize,
		})
	}

	if req.Shuffle {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(streamItems), func(i, j int) { streamItems[i], streamItems[j] = streamItems[j], streamItems[i] })
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	client := NewPlexClient(cfg)
	clientName := strings.TrimSpace(req.ClientName)
	if clientName == "" {
		clientName = cfg.TargetClientName
	}
	targetName, err := client.PlayStreamItemsOnTV(ctx, streamItems, clientName)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"count":  len(streamItems),
			"target": targetName,
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

	plexMovies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "plex library unavailable: " + err.Error()})
		return
	}

	var cacheStats DiscoveryCacheStats
	items, err := AnalyzeFilmography(ctx, cfg, client, plexMovies, person, role, playlistTitle, director, coActor, minYear, maxYear, minRating, &cacheStats)
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

// handleDiscoveryStudio: GET /api/discovery/studio?company=A24&minYear=&maxYear=&minRating=0
func (s *Server) handleDiscoveryStudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	company := strings.TrimSpace(r.URL.Query().Get("company"))
	if company == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "company query is required"})
		return
	}
	minYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("minYear")))
	maxYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("maxYear")))
	minRating, _ := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("minRating")), 64)

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	plexMovies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "plex library unavailable: " + err.Error()})
		return
	}

	items, resolvedName, err := AnalyzeStudio(ctx, cfg, client, plexMovies, company, minYear, maxYear, minRating)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"company": resolvedName,
			"items":   items,
		},
	})
}

// handleDiscoveryStart kicks off a filmography or studio analysis in the
// background and returns a jobId immediately. The caller polls
// /api/discovery/poll?jobId=<id> until State == "done" or "error".
// This avoids holding a long-lived HTTP connection open, which causes browsers
// to show "this tab is taking too long" dialogs.
func (s *Server) handleDiscoveryStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	var req struct {
		Mode          string  `json:"mode"`
		Person        string  `json:"person"`
		Role          string  `json:"role"`
		PlaylistTitle string  `json:"playlistTitle"`
		Director      string  `json:"director"`
		CoActor       string  `json:"coActor"`
		Company       string  `json:"company"`
		MinYear       int     `json:"minYear"`
		MaxYear       int     `json:"maxYear"`
		MinRating     float64 `json:"minRating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON"})
		return
	}

	job := s.discJobs.create()
	cfg := s.snapshot()
	client := NewPlexClient(cfg)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		s.discJobs.setRunning(job, "Loading library…")
		plexMovies, err := s.cachedListMovies(ctx)
		if err != nil {
			s.discJobs.setFailed(job, "Plex library unavailable: "+err.Error())
			return
		}

		if req.Mode == "studio" {
			s.discJobs.setRunning(job, "Searching TMDB for "+req.Company+"…")
			items, resolvedName, err := AnalyzeStudio(ctx, cfg, client, plexMovies, req.Company, req.MinYear, req.MaxYear, req.MinRating)
			if err != nil {
				s.discJobs.setFailed(job, err.Error())
				return
			}
			missing := 0
			for _, it := range items {
				if !it.InLibrary {
					missing++
				}
			}
			s.discJobs.setDone(job, map[string]any{
				"company": resolvedName,
				"total":   len(items),
				"missing": missing,
				"items":   items,
			})
		} else {
			s.discJobs.setRunning(job, "Fetching TMDB filmography for "+req.Person+"…")
			var cacheStats DiscoveryCacheStats
			items, err := AnalyzeFilmography(ctx, cfg, client, plexMovies, req.Person, req.Role,
				req.PlaylistTitle, req.Director, req.CoActor, req.MinYear, req.MaxYear, req.MinRating, &cacheStats)
			if err != nil {
				s.discJobs.setFailed(job, err.Error())
				return
			}
			missing := 0
			for _, it := range items {
				if !it.InLibrary {
					missing++
				}
			}
			s.discJobs.setDone(job, map[string]any{
				"person":        req.Person,
				"role":          req.Role,
				"playlistTitle": req.PlaylistTitle,
				"total":         len(items),
				"missing":       missing,
				"items":         items,
				"cache":         cacheStats,
			})
		}
	}()

	respondJSON(w, http.StatusAccepted, apiResponse{
		Success: true,
		Data:    map[string]any{"jobId": job.ID},
	})
}

// handleDiscoveryPoll returns the current state of a background discovery job.
func (s *Server) handleDiscoveryPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	jobID := r.URL.Query().Get("jobId")
	if jobID == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "jobId required"})
		return
	}
	job, ok := s.discJobs.get(jobID)
	if !ok {
		respondJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "job not found"})
		return
	}

	s.discJobs.mu.Lock()
	state := job.State
	message := job.Message
	result := job.Result
	errMsg := job.ErrMsg
	s.discJobs.mu.Unlock()

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"state":   state,
			"message": message,
			"result":  result,
			"error":   errMsg,
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	suggestions, err := SuggestPeople(ctx, cfg, query)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	movies, _ := s.cachedListMovies(ctx)
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
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	movies, err := s.cachedListMovies(ctx)
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

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	movies, err := s.cachedListMovies(ctx)
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
