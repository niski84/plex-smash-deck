package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiscoveryItem struct {
	TMDBID             int      `json:"tmdbId"`
	Title              string   `json:"title"`
	Year               int      `json:"year"`
	KnownFor           string   `json:"knownFor"`
	Overview           string   `json:"overview"`
	Genres             []string `json:"genres"`
	VoteAverage        float64  `json:"voteAverage"`
	PosterURL          string   `json:"posterUrl"`
	PosterPath         string   `json:"posterPath"` // raw TMDB path; UI can build /api/discovery/poster when posterUrl is empty
	InLibrary          bool     `json:"inLibrary"`
	InPlaylist         bool     `json:"inPlaylist"`
	RecommendationNo   int      `json:"recommendationNo"`
}

type RadarrAddItem struct {
	TMDBID int    `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
}

type RadarrAddResult struct {
	Added  []string          `json:"added"`
	Failed map[string]string `json:"failed"`
}

// Word-boundary match for "documentary" / "documentaries" in title or overview (avoids junk substrings).
var reDiscoveryDocWord = regexp.MustCompile(`(?i)\b(documentary|documentaries)\b`)

// "Making of" as its own phrase (avoids matching inside "remaking of").
var reDiscoveryMakingOf = regexp.MustCompile(`(?i)\bmaking of\b`)

// excludedFromDiscovery drops documentaries, TV / made-for-TV, news, music/concert films,
// and typical bonus-feature titles (making-of, behind the scenes) using TMDB genres plus
// title/overview heuristics.
func excludedFromDiscovery(title, overview string, genres []string) bool {
	for _, g := range genres {
		switch strings.ToLower(strings.TrimSpace(g)) {
		case "documentary", "tv movie", "news", "music":
			return true
		}
	}
	if reDiscoveryDocWord.MatchString(title) || reDiscoveryDocWord.MatchString(overview) {
		return true
	}
	combined := strings.TrimSpace(title + " " + overview)
	if reDiscoveryMakingOf.MatchString(combined) {
		return true
	}
	combinedLower := strings.ToLower(combined)
	phrases := []string{
		"behind the scenes",
		"behind-the-scenes",
		"behind the scene",
		"tv special",
		"television special",
		"made-for-television",
		"made for television",
		"miniseries",
		"mini-series",
	}
	for _, p := range phrases {
		if strings.Contains(combinedLower, p) {
			return true
		}
	}
	return false
}

// AnalyzeFilmography cross-references TMDB credits for a person against the
// caller-supplied Plex library slice (plexMovies). The caller is responsible for
// providing an up-to-date (or cached) snapshot — this function never hits Plex.
func AnalyzeFilmography(ctx context.Context, cfg Config, plex *PlexClient, plexMovies []Movie, personName, role, playlistTitle, directorFilter, coActorFilter string, minYear, maxYear int, minVoteAverage float64, stats *DiscoveryCacheStats) ([]DiscoveryItem, error) {
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	cache := newDiskDiscoveryCache(defaultDiscoveryCacheDir())

	personID, err := resolvePersonID(ctx, cfg.TMDBAPIKey, personName, cache, stats)
	if err != nil {
		return nil, err
	}
	credits, err := loadFilmography(ctx, cfg.TMDBAPIKey, personID, role, cache, stats)
	if err != nil {
		return nil, err
	}

	// plexTitleYears maps normalized title → all years that title exists in Plex.
	// This lets us do a fuzzy ±2-year match so that slight release-year discrepancies
	// between TMDB and Plex metadata don't cause a film to appear missing.
	plexTitleYears := make(map[string][]int, len(plexMovies))
	for _, movie := range plexMovies {
		key := normalizeTitle(movie.Title)
		plexTitleYears[key] = append(plexTitleYears[key], movie.Year)
	}

	inPlaylist := map[string]struct{}{}
	if strings.TrimSpace(playlistTitle) != "" {
		inPlaylist, _ = plex.PlaylistMovieTitles(ctx, playlistTitle)
	}

	directorFilter = strings.TrimSpace(directorFilter)
	coActorFilter = strings.TrimSpace(coActorFilter)
	creditCache := map[int]tmdbMovieCredits{}
	filtered := make([]tmdbCredit, 0, len(credits))
	for _, credit := range credits {
		if minYear > 0 && credit.Year > 0 && credit.Year < minYear {
			continue
		}
		if maxYear > 0 && credit.Year > 0 && credit.Year > maxYear {
			continue
		}

		if directorFilter != "" || coActorFilter != "" {
			creditsForMovie, ok := creditCache[credit.ID]
			if !ok {
				var fetchErr error
				creditsForMovie, fetchErr = tmdbMovieCreditsForMovieCached(ctx, cfg.TMDBAPIKey, credit.ID, cache, stats)
				if fetchErr != nil {
					continue
				}
				creditCache[credit.ID] = creditsForMovie
			}
			if directorFilter != "" && !containsIgnoreCase(creditsForMovie.Directors, directorFilter) {
				continue
			}
			if coActorFilter != "" && !containsIgnoreCase(creditsForMovie.Actors, coActorFilter) {
				continue
			}
		}
		filtered = append(filtered, credit)
	}

	detailIDs := uniqueMovieIDs(filtered)
	detailsMap := fetchMovieDetailsBatch(ctx, cfg.TMDBAPIKey, detailIDs, cache, stats)

	today := time.Now().Truncate(24 * time.Hour)

	items := make([]DiscoveryItem, 0, len(filtered))
	recNo := 1
	for _, credit := range filtered {
		// Drop films with a known future release date.
		if credit.ReleaseDate != "" {
			if rd, err := time.Parse("2006-01-02", credit.ReleaseDate); err == nil && rd.After(today) {
				continue
			}
		}

		det, hasDet := detailsMap[credit.ID]
		if minVoteAverage > 0 && hasDet && det.OK {
			if det.VoteAverage+1e-9 < minVoteAverage {
				continue
			}
		}

		// Drop short films / featurettes (runtime known and ≤ 50 minutes).
		if hasDet && det.OK && det.Runtime > 0 && det.Runtime <= 50 {
			continue
		}

		// Drop non-English-language films (original_language from TMDB).
		if hasDet && det.OK && det.OriginalLanguage != "" && det.OriginalLanguage != "en" {
			continue
		}

		// Use the richer /movie/{id} synopsis for filtering; fall back to credit overview.
		overview := strings.TrimSpace(credit.Overview)
		if hasDet && det.OK && det.Overview != "" {
			overview = det.Overview
		}
		genres := []string{}
		if hasDet && det.OK && len(det.Genres) > 0 {
			genres = append(genres, det.Genres...)
		}
		vote := 0.0
		rawPosterPath := mergePosterRawPath(credit, det, hasDet)
		posterURL := ""
		if hasDet && det.OK {
			vote = det.VoteAverage
			posterURL = det.PosterURL
		}
		if posterURL == "" && rawPosterPath != "" {
			posterURL = tmdbPosterURLFromPath(rawPosterPath)
		}

		// Drop documentaries, TV movies, news, making-of extras, and any film
		// whose overview mentions "documentary".
		if excludedFromDiscovery(credit.Title, overview, genres) {
			continue
		}

		hasLibrary := titleYearInLibrary(plexTitleYears, credit.Title, credit.Year)
		key := normalizeTitleYear(credit.Title, strconv.Itoa(credit.Year))
		_, hasPlaylist := inPlaylist[key]
		items = append(items, DiscoveryItem{
			TMDBID:             credit.ID,
			Title:              credit.Title,
			Year:               credit.Year,
			KnownFor:           credit.KnownFor,
			Overview:           overview,
			Genres:             genres,
			VoteAverage:        vote,
			PosterURL:          posterURL,
			PosterPath:         rawPosterPath,
			InLibrary:          hasLibrary,
			InPlaylist:         hasPlaylist,
			RecommendationNo:   recNo,
		})
		recNo++
	}

	return items, nil
}

func mergePosterRawPath(credit tmdbCredit, det fetchedMovieDetails, hasDet bool) string {
	raw := strings.TrimSpace(credit.PosterPath)
	if hasDet && det.OK && raw == "" {
		raw = strings.TrimSpace(det.PosterPath)
	}
	return raw
}

func resolvePersonID(ctx context.Context, apiKey, personName string, cache *diskDiscoveryCache, stats *DiscoveryCacheStats) (int, error) {
	personName = strings.TrimSpace(personName)
	if id, ok := cache.getPersonID(personName); ok {
		if stats != nil {
			stats.PersonIDHit = true
		}
		return id, nil
	}
	id, err := tmdbFindPersonID(ctx, apiKey, personName)
	if err != nil {
		return 0, err
	}
	cache.putPersonID(personName, id)
	return id, nil
}

func loadFilmography(ctx context.Context, apiKey string, personID int, role string, cache *diskDiscoveryCache, stats *DiscoveryCacheStats) ([]tmdbCredit, error) {
	var list []tmdbCredit
	if cached, ok := cache.getFilmography(personID, role); ok {
		if stats != nil {
			stats.FilmographyHit = true
		}
		list = cached
	} else {
		fresh, err := tmdbFilmography(ctx, apiKey, personID, role)
		if err != nil {
			return nil, err
		}
		cache.putFilmography(personID, role, fresh)
		list = fresh
	}
	// Apply exclusion rules regardless of whether the list came from cache or the
	// API — this ensures stale cached entries are filtered when rules change.
	filtered := list[:0]
	for _, c := range list {
		if c.Video {
			continue
		}
		if isMinorCastRole(c.KnownFor) {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered, nil
}

func tmdbMovieCreditsForMovieCached(ctx context.Context, apiKey string, movieID int, cache *diskDiscoveryCache, stats *DiscoveryCacheStats) (tmdbMovieCredits, error) {
	if mc, ok := cache.getMovieCredits(movieID); ok {
		if stats != nil {
			stats.CreditsHits++
		}
		return mc, nil
	}
	if stats != nil {
		stats.CreditsMisses++
	}
	mc, err := tmdbMovieCreditsForMovie(ctx, apiKey, movieID)
	if err != nil {
		return mc, err
	}
	cache.putMovieCredits(movieID, mc)
	return mc, nil
}

type fetchedMovieDetails struct {
	OK               bool
	Overview         string
	Genres           []string
	VoteAverage      float64
	Runtime          int    // minutes; 0 means unknown
	PosterURL        string
	PosterPath       string // raw TMDB poster_path
	OriginalLanguage string // ISO 639-1, e.g. "en", "fr", "it"
}

func uniqueMovieIDs(credits []tmdbCredit) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(credits))
	for _, c := range credits {
		if _, ok := seen[c.ID]; ok {
			continue
		}
		seen[c.ID] = struct{}{}
		out = append(out, c.ID)
	}
	return out
}

func fetchMovieDetailsBatch(ctx context.Context, apiKey string, ids []int, cache *diskDiscoveryCache, stats *DiscoveryCacheStats) map[int]fetchedMovieDetails {
	out := make(map[int]fetchedMovieDetails)
	if len(ids) == 0 {
		return out
	}
	missing := make([]int, 0, len(ids))
	for _, id := range ids {
		if det, ok := cache.getMovieDetails(id); ok && det.OK {
			out[id] = det
			if stats != nil {
				stats.MovieDetailsHits++
			}
			continue
		}
		missing = append(missing, id)
		if stats != nil {
			stats.MovieDetailsMisses++
		}
	}
	if len(missing) == 0 {
		return out
	}
	var mu sync.Mutex
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for _, id := range missing {
		wg.Add(1)
		go func(movieID int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d, err := tmdbFetchMovieDetails(ctx, apiKey, movieID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				det := fetchedMovieDetails{OK: false}
				out[movieID] = det
				return
			}
		det := fetchedMovieDetails{
			OK:               true,
			Overview:         d.Overview,
			Genres:           d.GenreNames,
			VoteAverage:      d.VoteAverage,
			Runtime:          d.Runtime,
			PosterURL:        d.PosterURL,
			PosterPath:       strings.TrimSpace(d.PosterPath),
			OriginalLanguage: d.OriginalLanguage,
		}
			out[movieID] = det
			cache.putMovieDetails(movieID, det)
		}(id)
	}
	wg.Wait()
	return out
}

type tmdbMovieDetailsResult struct {
	Overview         string
	GenreNames       []string
	VoteAverage      float64
	Runtime          int // minutes
	PosterURL        string
	PosterPath       string
	OriginalLanguage string
}

func tmdbFetchMovieDetails(ctx context.Context, apiKey string, movieID int) (tmdbMovieDetailsResult, error) {
	var empty tmdbMovieDetailsResult
	endpoint := fmt.Sprintf("https://api.themoviedb.org/3/movie/%d", movieID)
	q := url.Values{}
	q.Set("api_key", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return empty, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return empty, fmt.Errorf("tmdb movie %d: status %s", movieID, resp.Status)
	}
	var body struct {
		Overview         string  `json:"overview"`
		VoteAverage      float64 `json:"vote_average"`
		PosterPath       string  `json:"poster_path"`
		Runtime          int     `json:"runtime"`
		OriginalLanguage string  `json:"original_language"`
		Genres           []struct {
			Name string `json:"name"`
		} `json:"genres"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return empty, err
	}
	names := make([]string, 0, len(body.Genres))
	for _, g := range body.Genres {
		n := strings.TrimSpace(g.Name)
		if n != "" {
			names = append(names, n)
		}
	}
	rawPath := strings.TrimSpace(body.PosterPath)
	posterURL := tmdbPosterURLFromPath(body.PosterPath)
	return tmdbMovieDetailsResult{
		Overview:         strings.TrimSpace(body.Overview),
		GenreNames:       names,
		VoteAverage:      body.VoteAverage,
		Runtime:          body.Runtime,
		PosterURL:        posterURL,
		PosterPath:       rawPath,
		OriginalLanguage: strings.TrimSpace(body.OriginalLanguage),
	}, nil
}

type Collaborators struct {
	SuggestedRole string   `json:"suggestedRole"`
	Directors     []string `json:"directors"`
	Actors        []string `json:"actors"`
}

func DiscoverCollaborators(plexMovies []Movie, person string) Collaborators {
	person = strings.TrimSpace(person)
	if person == "" {
		return Collaborators{SuggestedRole: "", Directors: []string{}, Actors: []string{}}
	}

	actorHits := 0
	directorHits := 0
	directorsSet := map[string]struct{}{}
	actorsSet := map[string]struct{}{}

	for _, movie := range plexMovies {
		isActor := containsIgnoreCase(movie.Actors, person)
		isDirector := containsIgnoreCase(movie.Directors, person)
		if isActor {
			actorHits++
			for _, director := range movie.Directors {
				if !strings.EqualFold(strings.TrimSpace(director), person) && strings.TrimSpace(director) != "" {
					directorsSet[director] = struct{}{}
				}
			}
			for _, actor := range movie.Actors {
				if !strings.EqualFold(strings.TrimSpace(actor), person) && strings.TrimSpace(actor) != "" {
					actorsSet[actor] = struct{}{}
				}
			}
		}
		if isDirector {
			directorHits++
			for _, actor := range movie.Actors {
				if strings.TrimSpace(actor) != "" {
					actorsSet[actor] = struct{}{}
				}
			}
		}
	}

	role := ""
	if actorHits > directorHits {
		role = "actor"
	} else if directorHits > actorHits {
		role = "director"
	}

	directors := setToSortedSlice(directorsSet)
	actors := setToSortedSlice(actorsSet)
	return Collaborators{SuggestedRole: role, Directors: directors, Actors: actors}
}

func SuggestPeople(ctx context.Context, cfg Config, query string) ([]string, error) {
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return []string{}, nil
	}

	endpoint := "https://api.themoviedb.org/3/search/person"
	params := url.Values{}
	params.Set("api_key", cfg.TMDBAPIKey)
	params.Set("query", query)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(body.Results))
	for _, result := range body.Results {
		name := strings.TrimSpace(result.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
		if len(out) >= 10 {
			break
		}
	}
	return out, nil
}

func AddMoviesToRadarr(ctx context.Context, cfg Config, items []RadarrAddItem) (RadarrAddResult, error) {
	if strings.TrimSpace(cfg.RadarrURL) == "" || strings.TrimSpace(cfg.RadarrAPIKey) == "" {
		return RadarrAddResult{}, fmt.Errorf("radarr settings missing URL or API key")
	}
	if strings.TrimSpace(cfg.RadarrRootFolder) == "" {
		return RadarrAddResult{}, fmt.Errorf("radarr root folder is required")
	}

	client := &http.Client{Timeout: 25 * time.Second}
	result := RadarrAddResult{
		Added:  []string{},
		Failed: map[string]string{},
	}

	for _, item := range items {
		payload := map[string]any{
			"title":            item.Title,
			"qualityProfileId": cfg.RadarrProfileID,
			"tmdbId":           item.TMDBID,
			"year":             item.Year,
			"rootFolderPath":   cfg.RadarrRootFolder,
			"monitored":        true,
			"addOptions": map[string]any{
				"searchForMovie": true,
			},
		}

		encoded, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.RadarrURL, "/")+"/api/v3/movie", strings.NewReader(string(encoded)))
		if err != nil {
			result.Failed[item.Title] = err.Error()
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", cfg.RadarrAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			result.Failed[item.Title] = err.Error()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			result.Failed[item.Title] = "radarr rejected request"
		} else {
			result.Added = append(result.Added, item.Title)
		}
		resp.Body.Close()
	}
	return result, nil
}

type tmdbCredit struct {
	ID          int
	Title       string
	Year        int
	ReleaseDate string // ISO-8601 e.g. "2024-03-15"; empty when TMDB has no date
	KnownFor    string
	Overview    string
	PosterPath  string // from movie_credits poster_path; fallback when /movie/{id} fails
	Video       bool   // TMDB "video" flag: true = direct-to-video / bonus content / supplement
}

type tmdbMovieCredits struct {
	Directors []string
	Actors    []string
}

func tmdbFindPersonID(ctx context.Context, apiKey, personName string) (int, error) {
	personName = strings.TrimSpace(personName)
	if personName == "" {
		return 0, fmt.Errorf("person name is required")
	}
	endpoint := "https://api.themoviedb.org/3/search/person"
	query := url.Values{}
	query.Set("api_key", apiKey)
	query.Set("query", personName)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("tmdb search/person: %s — %s", resp.Status, truncateErrBody(bodyBytes))
	}

	var body struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return 0, err
	}
	if len(body.Results) == 0 {
		return 0, fmt.Errorf("no TMDB person found for %q", personName)
	}
	return body.Results[0].ID, nil
}

func tmdbFilmography(ctx context.Context, apiKey string, personID int, role string) ([]tmdbCredit, error) {
	endpoint := fmt.Sprintf("https://api.themoviedb.org/3/person/%d/movie_credits", personID)
	query := url.Values{}
	query.Set("api_key", apiKey)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tmdb movie_credits: %s — %s", resp.Status, truncateErrBody(bodyBytes))
	}

	var body struct {
		Cast []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			Character   string `json:"character"`
			Overview    string `json:"overview"`
			PosterPath  string `json:"poster_path"`
			Video       bool   `json:"video"`
		} `json:"cast"`
		Crew []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			Job         string `json:"job"`
			Overview    string `json:"overview"`
			PosterPath  string `json:"poster_path"`
			Video       bool   `json:"video"`
		} `json:"crew"`
	}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, err
	}

	role = strings.ToLower(strings.TrimSpace(role))
	dedup := map[int]tmdbCredit{}
	if role == "" || role == "actor" {
		for _, cast := range body.Cast {
			if cast.Video {
				continue // skip direct-to-video / bonus content
			}
			if isMinorCastRole(cast.Character) {
				continue // skip uncredited cameos and voice-only appearances
			}
			dedup[cast.ID] = tmdbCredit{
				ID:          cast.ID,
				Title:       cast.Title,
				Year:        yearFromDate(cast.ReleaseDate),
				ReleaseDate: strings.TrimSpace(cast.ReleaseDate),
				KnownFor:    cast.Character,
				Overview:    cast.Overview,
				PosterPath:  strings.TrimSpace(cast.PosterPath),
			}
		}
	}
	if role == "" || role == "director" {
		for _, crew := range body.Crew {
			if strings.ToLower(crew.Job) != "director" {
				continue
			}
			if crew.Video {
				continue // skip direct-to-video / bonus content
			}
			pc := strings.TrimSpace(crew.PosterPath)
			if ex, ok := dedup[crew.ID]; ok {
				dedup[crew.ID] = tmdbCredit{
					ID:          crew.ID,
					Title:       crew.Title,
					Year:        yearFromDate(crew.ReleaseDate),
					ReleaseDate: strings.TrimSpace(crew.ReleaseDate),
					KnownFor:    crew.Job,
					Overview:    crew.Overview,
					PosterPath:  pickPosterPath(ex.PosterPath, pc),
				}
				continue
			}
			dedup[crew.ID] = tmdbCredit{
				ID:          crew.ID,
				Title:       crew.Title,
				Year:        yearFromDate(crew.ReleaseDate),
				ReleaseDate: strings.TrimSpace(crew.ReleaseDate),
				KnownFor:    crew.Job,
				Overview:    crew.Overview,
				PosterPath:  pc,
			}
		}
	}

	out := make([]tmdbCredit, 0, len(dedup))
	for _, item := range dedup {
		out = append(out, item)
	}
	sortCredits(out)
	return out, nil
}

func tmdbMovieCreditsForMovie(ctx context.Context, apiKey string, movieID int) (tmdbMovieCredits, error) {
	endpoint := fmt.Sprintf("https://api.themoviedb.org/3/movie/%d/credits", movieID)
	query := url.Values{}
	query.Set("api_key", apiKey)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return tmdbMovieCredits{}, err
	}
	defer resp.Body.Close()

	var body struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return tmdbMovieCredits{}, err
	}

	actorSet := map[string]struct{}{}
	directorSet := map[string]struct{}{}
	for _, cast := range body.Cast {
		if strings.TrimSpace(cast.Name) != "" {
			actorSet[cast.Name] = struct{}{}
		}
	}
	for _, crew := range body.Crew {
		if strings.EqualFold(strings.TrimSpace(crew.Job), "director") && strings.TrimSpace(crew.Name) != "" {
			directorSet[crew.Name] = struct{}{}
		}
	}

	return tmdbMovieCredits{
		Directors: setToSortedSlice(directorSet),
		Actors:    setToSortedSlice(actorSet),
	}, nil
}

func sortCredits(items []tmdbCredit) {
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Year == 0 || (items[j].Year != 0 && items[j].Year < items[i].Year) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func yearFromDate(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(date[:4])
	return year
}

func pickPosterPath(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a != "" {
		return a
	}
	return b
}

// tmdbPosterURLFromPath builds a w185 image URL from TMDB poster_path.
// Paths are usually like "/abc.jpg"; if the leading slash is missing, the URL is invalid (404).
func tmdbPosterURLFromPath(posterPath string) string {
	s := strings.TrimSpace(posterPath)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return "https://image.tmdb.org/t/p/w185" + s
}

func truncateErrBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 280 {
		return s[:280] + "…"
	}
	return s
}

// normalizeTitle normalizes a movie title for comparison (lowercase, strip punctuation).
func normalizeTitle(title string) string {
	clean := strings.ToLower(strings.TrimSpace(title))
	clean = strings.ReplaceAll(clean, ":", "")
	clean = strings.ReplaceAll(clean, "-", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	return clean
}

// titleYearInLibrary returns true if the Plex library contains a movie with the
// same normalized title whose year is within ±2 of the given TMDB year.
// This tolerates the common case where TMDB and Plex metadata disagree on the
// release year by one or two years (e.g. festival year vs. wide-release year).
func titleYearInLibrary(plexTitleYears map[string][]int, title string, tmdbYear int) bool {
	key := normalizeTitle(title)
	years, ok := plexTitleYears[key]
	if !ok {
		return false
	}
	if tmdbYear == 0 {
		return true // no year from TMDB — title match alone is sufficient
	}
	for _, plexYear := range years {
		if plexYear == 0 || abs(plexYear-tmdbYear) <= 2 {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func normalizeTitleYear(title, year string) string {
	clean := strings.ToLower(strings.TrimSpace(title))
	clean = strings.ReplaceAll(clean, ":", "")
	clean = strings.ReplaceAll(clean, "-", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	year = strings.TrimSpace(year)
	if clean == "" {
		return ""
	}
	if year == "" || year == "0" {
		return clean
	}
	return clean + "|" + year
}

// isMinorCastRole returns true for character strings that indicate an
// uncredited cameo or a voice-only appearance — roles too minor or peripheral
// to count as a real acting credit for Discovery purposes.
func isMinorCastRole(character string) bool {
	ch := strings.ToLower(character)
	return strings.Contains(ch, "(uncredited)") || strings.Contains(ch, "(voice)")
}

func containsIgnoreCase(values []string, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func setToSortedSlice(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if strings.ToLower(out[j]) < strings.ToLower(out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// AnalyzeStudio finds TMDB movies from a production company and cross-references
// them against the caller-supplied Plex library slice. This function never hits Plex.
func AnalyzeStudio(ctx context.Context, cfg Config, _ *PlexClient, plexMovies []Movie, companyName string, minYear, maxYear int, minVoteAverage float64) ([]DiscoveryItem, string, error) {
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, "", fmt.Errorf("TMDB API key not configured")
	}
	companyName = strings.TrimSpace(companyName)
	if companyName == "" {
		return nil, "", fmt.Errorf("company name is required")
	}

	// 1. Resolve company name → TMDB company ID.
	companyID, resolvedName, err := tmdbFindCompanyID(ctx, cfg.TMDBAPIKey, companyName)
	if err != nil {
		return nil, "", err
	}

	// 2. Fetch movies via /discover/movie?with_companies=ID (sorted by vote_count desc, paginated).
	credits, err := tmdbDiscoverByCompany(ctx, cfg.TMDBAPIKey, companyID, minYear, maxYear)
	if err != nil {
		return nil, resolvedName, err
	}

	// 3. Build Plex library index from pre-fetched list.
	plexTitleYears := make(map[string][]int, len(plexMovies))
	for _, m := range plexMovies {
		key := normalizeTitle(m.Title)
		plexTitleYears[key] = append(plexTitleYears[key], m.Year)
	}

	today := time.Now().Truncate(24 * time.Hour)
	items := make([]DiscoveryItem, 0, len(credits))
	recNo := 1

	for _, c := range credits {
		// Drop future releases.
		if c.ReleaseDate != "" {
			if rd, err2 := time.Parse("2006-01-02", c.ReleaseDate); err2 == nil && rd.After(today) {
				continue
			}
		}
		if minVoteAverage > 0 && c.VoteAverage+1e-9 < minVoteAverage {
			continue
		}
		// Drop short films / featurettes.
		if c.Runtime > 0 && c.Runtime <= 50 {
			continue
		}
		// English only.
		if c.OriginalLanguage != "" && c.OriginalLanguage != "en" {
			continue
		}

		genres := c.Genres
		if excludedFromDiscovery(c.Title, c.Overview, genres) {
			continue
		}

		posterURL := tmdbPosterURLFromPath(c.PosterPath)
		hasLibrary := titleYearInLibrary(plexTitleYears, c.Title, c.Year)

		items = append(items, DiscoveryItem{
			TMDBID:           c.TMDBID,
			Title:            c.Title,
			Year:             c.Year,
			KnownFor:         resolvedName,
			Overview:         c.Overview,
			Genres:           genres,
			VoteAverage:      c.VoteAverage,
			PosterURL:        posterURL,
			PosterPath:       c.PosterPath,
			InLibrary:        hasLibrary,
			RecommendationNo: recNo,
		})
		recNo++
	}

	return items, resolvedName, nil
}

// tmdbDiscoverMovie is the raw result from /discover/movie.
type tmdbDiscoverMovie struct {
	TMDBID           int      `json:"id"`
	Title            string   `json:"title"`
	Year             int      // parsed from release_date
	ReleaseDate      string   `json:"release_date"`
	Overview         string   `json:"overview"`
	VoteAverage      float64  `json:"vote_average"`
	PosterPath       string   `json:"poster_path"`
	OriginalLanguage string   `json:"original_language"`
	Runtime          int      // populated from /movie/{id} details if needed
	Genres           []string // genre names resolved from genre_ids
}

// tmdbFindCompanyID searches for a production company by name and returns its TMDB ID and canonical name.
func tmdbFindCompanyID(ctx context.Context, apiKey, companyName string) (int, string, error) {
	u := "https://api.themoviedb.org/3/search/company?api_key=" + url.QueryEscape(apiKey) +
		"&query=" + url.QueryEscape(companyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("TMDB company search %d: %s", resp.StatusCode, truncateErrBody(body))
	}
	var result struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, "", fmt.Errorf("decode company search: %w", err)
	}
	if len(result.Results) == 0 {
		return 0, "", fmt.Errorf("no company found matching %q", companyName)
	}
	top := result.Results[0]
	return top.ID, top.Name, nil
}

// tmdbGenreMap fetches the TMDB genre id→name map for movies.
func tmdbGenreMap(ctx context.Context, apiKey string) (map[int]string, error) {
	u := "https://api.themoviedb.org/3/genre/movie/list?api_key=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB genre list %d", resp.StatusCode)
	}
	var result struct {
		Genres []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"genres"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	m := make(map[int]string, len(result.Genres))
	for _, g := range result.Genres {
		m[g.ID] = g.Name
	}
	return m, nil
}

// tmdbDiscoverByCompany pages through /discover/movie for a given company ID.
// It fetches up to 10 pages (200 movies) sorted by vote_count descending.
func tmdbDiscoverByCompany(ctx context.Context, apiKey string, companyID, minYear, maxYear int) ([]tmdbDiscoverMovie, error) {
	genreMap, err := tmdbGenreMap(ctx, apiKey)
	if err != nil {
		// Non-fatal — genres will be empty strings but won't crash.
		genreMap = map[int]string{}
	}

	var all []tmdbDiscoverMovie
	const maxPages = 10
	type discoverPage struct {
		Results []struct {
			ID               int     `json:"id"`
			Title            string  `json:"title"`
			ReleaseDate      string  `json:"release_date"`
			Overview         string  `json:"overview"`
			VoteAverage      float64 `json:"vote_average"`
			PosterPath       string  `json:"poster_path"`
			OriginalLanguage string  `json:"original_language"`
			GenreIDs         []int   `json:"genre_ids"`
		} `json:"results"`
		TotalPages int `json:"total_pages"`
	}

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		u := fmt.Sprintf(
			"https://api.themoviedb.org/3/discover/movie?api_key=%s&with_companies=%d&sort_by=vote_count.desc&page=%d",
			url.QueryEscape(apiKey), companyID, pageNum,
		)
		if minYear > 0 {
			u += fmt.Sprintf("&primary_release_date.gte=%d-01-01", minYear)
		}
		if maxYear > 0 {
			u += fmt.Sprintf("&primary_release_date.lte=%d-12-31", maxYear)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("TMDB discover %d: %s", resp.StatusCode, truncateErrBody(body))
		}

		var pg discoverPage
		if err := json.Unmarshal(body, &pg); err != nil {
			return nil, fmt.Errorf("decode discover page: %w", err)
		}

		for _, r := range pg.Results {
			year := 0
			if len(r.ReleaseDate) >= 4 {
				year, _ = strconv.Atoi(r.ReleaseDate[:4])
			}
			genres := make([]string, 0, len(r.GenreIDs))
			for _, gid := range r.GenreIDs {
				if name, ok := genreMap[gid]; ok {
					genres = append(genres, name)
				}
			}
			all = append(all, tmdbDiscoverMovie{
				TMDBID:           r.ID,
				Title:            r.Title,
				Year:             year,
				ReleaseDate:      r.ReleaseDate,
				Overview:         r.Overview,
				VoteAverage:      r.VoteAverage,
				PosterPath:       r.PosterPath,
				OriginalLanguage: r.OriginalLanguage,
				Genres:           genres,
			})
		}

		if pg.TotalPages <= pageNum || len(pg.Results) == 0 {
			break
		}
	}

	return all, nil
}
