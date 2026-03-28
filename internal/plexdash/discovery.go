package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiscoveryItem struct {
	TMDBID           int      `json:"tmdbId"`
	Title            string   `json:"title"`
	Year             int      `json:"year"`
	ReleaseDate      string   `json:"releaseDate,omitempty"` // ISO YYYY-MM-DD when known (TMDB)
	KnownFor         string   `json:"knownFor"`
	Overview         string   `json:"overview"`
	Genres           []string `json:"genres"`
	VoteAverage      float64  `json:"voteAverage"`
	PosterURL        string   `json:"posterUrl"`
	PosterPath       string   `json:"posterPath"` // raw TMDB path; UI can build /api/discovery/poster when posterUrl is empty
	InLibrary        bool     `json:"inLibrary"`
	PlexViewCount    *int     `json:"plexViewCount,omitempty"` // set when InLibrary; Plex aggregate play count
	InPlaylist       bool     `json:"inPlaylist"`
	RecommendationNo int      `json:"recommendationNo"`
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

// discoveryEffectiveMinVote maps the UI "min TMDB rating" to a floor on vote average.
// 0 or negative means no minimum (include all ratings).
func discoveryEffectiveMinVote(requested float64) float64 {
	if requested <= 0 {
		return 0
	}
	return requested
}

// buildPlexTMDBIndex maps TMDB id → Movie for rows where Plex exposed a TMDB id in guid.
func buildPlexTMDBIndex(plexMovies []Movie) map[int]Movie {
	out := make(map[int]Movie)
	for _, m := range plexMovies {
		if m.TMDBID > 0 {
			out[m.TMDBID] = m
		}
	}
	return out
}

// plexLibraryMatch decides if a TMDB credit is in the Plex snapshot: prefer TMDB id from Plex guid, else title/year.
func plexLibraryMatch(plexMovies []Movie, plexTitleYears map[string][]int, byTMDB map[int]Movie, tmdbMovieID int, title string, year int) (bool, Movie) {
	if tmdbMovieID > 0 {
		if m, ok := byTMDB[tmdbMovieID]; ok {
			return true, m
		}
	}
	if titleYearInLibrary(plexTitleYears, title, year) {
		if m, ok := findMatchingPlexMovie(plexMovies, title, year); ok {
			return true, m
		}
	}
	return false, Movie{}
}

// findMatchingPlexMovie returns the Plex library row that matches a TMDB title/year
// (same ±2 year tolerance as titleYearInLibrary). Used for Plex view counts.
func findMatchingPlexMovie(plexMovies []Movie, title string, tmdbYear int) (Movie, bool) {
	key := normalizeTitle(title)
	var candidates []Movie
	for _, m := range plexMovies {
		if normalizeTitle(m.Title) != key {
			continue
		}
		if tmdbYear == 0 {
			candidates = append(candidates, m)
			continue
		}
		if m.Year == 0 || abs(m.Year-tmdbYear) <= 2 {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return Movie{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	if tmdbYear == 0 {
		return candidates[0], true
	}
	best := candidates[0]
	bestDist := abs(best.Year - tmdbYear)
	for _, m := range candidates[1:] {
		d := abs(m.Year - tmdbYear)
		if d < bestDist {
			bestDist = d
			best = m
		}
	}
	return best, true
}

func discoveryReleaseSortKey(it DiscoveryItem) int64 {
	s := strings.TrimSpace(it.ReleaseDate)
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.Unix()
		}
	}
	if it.Year > 0 {
		return time.Date(it.Year, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	}
	return 0
}

func normalizeGenreIDs(ids []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

// genreKeyFromIDs returns a stable filename segment for cache keys, e.g. "12-28-35", or "" if empty.
func genreKeyFromIDs(ids []int) string {
	ids = normalizeGenreIDs(ids)
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, "-")
}

// movieMatchesGenreFilterOR is true if the movie has at least one of the TMDB genres (by name match).
func movieMatchesGenreFilterOR(movieGenreNames []string, filterIDs []int, idToName map[int]string) bool {
	if len(filterIDs) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(movieGenreNames))
	for _, g := range movieGenreNames {
		have[strings.ToLower(strings.TrimSpace(g))] = struct{}{}
	}
	for _, id := range filterIDs {
		n := strings.TrimSpace(idToName[id])
		if n == "" {
			continue
		}
		if _, ok := have[strings.ToLower(n)]; ok {
			return true
		}
	}
	return false
}

// tmdbDiscoverWithGenresQuery appends &with_genres= to TMDB discover URLs (pipe = OR between ids).
func tmdbDiscoverWithGenresQuery(ids []int) string {
	ids = normalizeGenreIDs(ids)
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return "&with_genres=" + url.QueryEscape(strings.Join(parts, "|"))
}

// browseStudioDiscoverExtraKey builds a cache filename segment for genre + min vote (e.g. "g12-28-r85", "r85").
func browseStudioDiscoverExtraKey(genreKey string, minVote float64) string {
	var parts []string
	if genreKey != "" {
		parts = append(parts, "g"+genreKey)
	}
	if v := discoveryEffectiveMinVote(minVote); v > 0 {
		parts = append(parts, fmt.Sprintf("r%d", int(v*10+0.5)))
	}
	return strings.Join(parts, "-")
}

// tmdbDiscoverVoteAverageGteQuery appends TMDB vote_average.gte so the API returns high-rated pages (not popularity then filter to zero).
func tmdbDiscoverVoteAverageGteQuery(minVote float64) string {
	v := discoveryEffectiveMinVote(minVote)
	if v <= 0 {
		return ""
	}
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return "&vote_average.gte=" + url.QueryEscape(s)
}

func sortDiscoveryItems(items []DiscoveryItem) {
	sort.SliceStable(items, func(i, j int) bool {
		ai, aj := discoveryReleaseSortKey(items[i]), discoveryReleaseSortKey(items[j])
		if ai != aj {
			return ai > aj
		}
		if items[i].VoteAverage != items[j].VoteAverage {
			return items[i].VoteAverage > items[j].VoteAverage
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
	for i := range items {
		items[i].RecommendationNo = i + 1
	}
}

// AnalyzeFilmography cross-references TMDB credits for a person against the
// caller-supplied Plex library slice (plexMovies). The caller is responsible for
// providing an up-to-date (or cached) snapshot — this function never hits Plex.
func AnalyzeFilmography(ctx context.Context, cfg Config, plex *PlexClient, plexMovies []Movie, personName, role, playlistTitle, directorFilter, coActorFilter string, minYear, maxYear int, minVoteAverage float64, genreFilterIDs []int, stats *DiscoveryCacheStats) ([]DiscoveryItem, error) {
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	cache := newDiskDiscoveryCache(defaultDiscoveryCacheDir())
	genreFilterIDs = normalizeGenreIDs(genreFilterIDs)
	var genreIDToName map[int]string
	if len(genreFilterIDs) > 0 {
		var gerr error
		genreIDToName, gerr = tmdbGenreMapWithCache(ctx, cfg.TMDBAPIKey, cache)
		if gerr != nil {
			genreIDToName = map[int]string{}
		}
	}

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
	plexByTMDB := buildPlexTMDBIndex(plexMovies)

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
	effectiveMin := discoveryEffectiveMinVote(minVoteAverage)

	items := make([]DiscoveryItem, 0, len(filtered))
	for _, credit := range filtered {
		// Drop films with a known future release date.
		if credit.ReleaseDate != "" {
			if rd, err := time.Parse("2006-01-02", credit.ReleaseDate); err == nil && rd.After(today) {
				continue
			}
		}

		det, hasDet := detailsMap[credit.ID]

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
		if len(genreFilterIDs) > 0 && !movieMatchesGenreFilterOR(genres, genreFilterIDs, genreIDToName) {
			continue
		}
		vote := credit.VoteAverage
		rawPosterPath := mergePosterRawPath(credit, det, hasDet)
		posterURL := ""
		if hasDet && det.OK {
			vote = det.VoteAverage
			posterURL = det.PosterURL
		}
		if posterURL == "" && rawPosterPath != "" {
			posterURL = tmdbPosterURLFromPath(rawPosterPath)
		}

		imdbID := ""
		if hasDet && det.OK {
			imdbID = strings.TrimSpace(det.IMDbID)
		}
		if cfg.OMDbBlendRatings && strings.TrimSpace(cfg.OMDbAPIKey) != "" && imdbID == "" {
			if ex, err := tmdbMovieExternalIDsCached(ctx, cfg.TMDBAPIKey, credit.ID, cache); err == nil {
				imdbID = strings.TrimSpace(ex)
			}
		}
		if cfg.OMDbBlendRatings && strings.TrimSpace(cfg.OMDbAPIKey) != "" {
			vote = blendVoteWithOMDb(ctx, cfg, vote, imdbID)
		}

		// Drop documentaries, TV movies, news, making-of extras, and any film
		// whose overview mentions "documentary".
		if excludedFromDiscovery(credit.Title, overview, genres) {
			continue
		}

		// TMDB vote average (0–10): optional minimum from UI (0 = any rating).
		if effectiveMin > 0 && vote+1e-9 < effectiveMin {
			continue
		}

		hasLibrary, pm := plexLibraryMatch(plexMovies, plexTitleYears, plexByTMDB, credit.ID, credit.Title, credit.Year)
		var plexVC *int
		if hasLibrary {
			vc := pm.ViewCount
			plexVC = &vc
		}
		key := normalizeTitleYear(credit.Title, strconv.Itoa(credit.Year))
		_, hasPlaylist := inPlaylist[key]
		items = append(items, DiscoveryItem{
			TMDBID:           credit.ID,
			Title:            credit.Title,
			Year:             credit.Year,
			ReleaseDate:      strings.TrimSpace(credit.ReleaseDate),
			KnownFor:         credit.KnownFor,
			Overview:         overview,
			Genres:           genres,
			VoteAverage:      vote,
			PosterURL:        posterURL,
			PosterPath:       rawPosterPath,
			InLibrary:        hasLibrary,
			PlexViewCount:    plexVC,
			InPlaylist:       hasPlaylist,
			RecommendationNo: 0,
		})
	}

	sortDiscoveryItems(items)

	return items, nil
}

func filterDiscoveryItemsByMinVote(items []DiscoveryItem, minVoteAverage float64) []DiscoveryItem {
	effectiveMin := discoveryEffectiveMinVote(minVoteAverage)
	if effectiveMin <= 0 {
		return items
	}
	out := make([]DiscoveryItem, 0, len(items))
	for _, it := range items {
		if it.VoteAverage+1e-9 >= effectiveMin {
			out = append(out, it)
		}
	}
	return out
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
	Runtime          int // minutes; 0 means unknown
	PosterURL        string
	PosterPath       string // raw TMDB poster_path
	OriginalLanguage string // ISO 639-1, e.g. "en", "fr", "it"
	IMDbID           string // from TMDB external_ids when present (e.g. tt1234567)
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
				IMDbID:           d.IMDbID,
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
	IMDbID           string
}

func tmdbFetchMovieDetails(ctx context.Context, apiKey string, movieID int) (tmdbMovieDetailsResult, error) {
	var empty tmdbMovieDetailsResult
	endpoint := fmt.Sprintf("https://api.themoviedb.org/3/movie/%d", movieID)
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("append_to_response", "external_ids")
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
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
		} `json:"external_ids"`
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
		IMDbID:           strings.TrimSpace(body.ExternalIDs.IMDbID),
	}, nil
}

func tmdbFetchMovieExternalIDs(ctx context.Context, apiKey string, movieID int) (string, error) {
	u := fmt.Sprintf("https://api.themoviedb.org/3/movie/%d/external_ids?api_key=%s", movieID, url.QueryEscape(apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("tmdb external_ids %d: status %s", movieID, resp.Status)
	}
	var body struct {
		IMDbID string `json:"imdb_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return strings.TrimSpace(body.IMDbID), nil
}

func tmdbMovieExternalIDsCached(ctx context.Context, apiKey string, movieID int, cache *diskDiscoveryCache) (string, error) {
	if cache != nil {
		if s, ok := cache.getMovieExternalIDs(movieID); ok {
			return s, nil
		}
	}
	s, err := tmdbFetchMovieExternalIDs(ctx, apiKey, movieID)
	if err != nil {
		return "", err
	}
	if cache != nil {
		cache.putMovieExternalIDs(movieID, s)
	}
	return s, nil
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
	ReleaseDate string  // ISO-8601 e.g. "2024-03-15"; empty when TMDB has no date
	VoteAverage float64 // from movie_credits vote_average when present
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
			ID          int     `json:"id"`
			Title       string  `json:"title"`
			ReleaseDate string  `json:"release_date"`
			Character   string  `json:"character"`
			Overview    string  `json:"overview"`
			PosterPath  string  `json:"poster_path"`
			VoteAverage float64 `json:"vote_average"`
			Video       bool    `json:"video"`
		} `json:"cast"`
		Crew []struct {
			ID          int     `json:"id"`
			Title       string  `json:"title"`
			ReleaseDate string  `json:"release_date"`
			Job         string  `json:"job"`
			Overview    string  `json:"overview"`
			PosterPath  string  `json:"poster_path"`
			VoteAverage float64 `json:"vote_average"`
			Video       bool    `json:"video"`
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
				VoteAverage: cast.VoteAverage,
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
				va := crew.VoteAverage
				if va < 1e-9 {
					va = ex.VoteAverage
				}
				dedup[crew.ID] = tmdbCredit{
					ID:          crew.ID,
					Title:       crew.Title,
					Year:        yearFromDate(crew.ReleaseDate),
					ReleaseDate: strings.TrimSpace(crew.ReleaseDate),
					VoteAverage: va,
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
				VoteAverage: crew.VoteAverage,
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

// normalizeTitle normalizes a movie title for comparison (lowercase, punctuation, & vs and).
func normalizeTitle(title string) string {
	clean := strings.ToLower(strings.TrimSpace(title))
	clean = strings.ReplaceAll(clean, "&", " and ")
	clean = strings.ReplaceAll(clean, "'", "")
	clean = strings.ReplaceAll(clean, "’", "")
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

// buildDiscoveryItemsFromTMDBDiscoverList turns TMDB /discover/movie rows into discovery rows vs Plex.
// knownFor is shown in the "Known for" column (studio name, year range label, etc.).
func buildDiscoveryItemsFromTMDBDiscoverList(plexMovies []Movie, credits []tmdbDiscoverMovie, knownFor string, minVoteAverage float64) []DiscoveryItem {
	plexTitleYears := make(map[string][]int, len(plexMovies))
	for _, m := range plexMovies {
		key := normalizeTitle(m.Title)
		plexTitleYears[key] = append(plexTitleYears[key], m.Year)
	}
	plexByTMDB := buildPlexTMDBIndex(plexMovies)

	today := time.Now().Truncate(24 * time.Hour)
	effectiveMin := discoveryEffectiveMinVote(minVoteAverage)
	items := make([]DiscoveryItem, 0, len(credits))

	for _, c := range credits {
		if c.ReleaseDate != "" {
			if rd, err2 := time.Parse("2006-01-02", c.ReleaseDate); err2 == nil && rd.After(today) {
				continue
			}
		}
		if effectiveMin > 0 && c.VoteAverage+1e-9 < effectiveMin {
			continue
		}
		if c.Runtime > 0 && c.Runtime <= 50 {
			continue
		}
		if c.OriginalLanguage != "" && c.OriginalLanguage != "en" {
			continue
		}

		genres := c.Genres
		if excludedFromDiscovery(c.Title, c.Overview, genres) {
			continue
		}

		posterURL := tmdbPosterURLFromPath(c.PosterPath)
		hasLibrary, pm := plexLibraryMatch(plexMovies, plexTitleYears, plexByTMDB, c.TMDBID, c.Title, c.Year)
		var plexVC *int
		if hasLibrary {
			vc := pm.ViewCount
			plexVC = &vc
		}

		items = append(items, DiscoveryItem{
			TMDBID:           c.TMDBID,
			Title:            c.Title,
			Year:             c.Year,
			ReleaseDate:      strings.TrimSpace(c.ReleaseDate),
			KnownFor:         knownFor,
			Overview:         c.Overview,
			Genres:           genres,
			VoteAverage:      c.VoteAverage,
			PosterURL:        posterURL,
			PosterPath:       c.PosterPath,
			InLibrary:        hasLibrary,
			PlexViewCount:    plexVC,
			RecommendationNo: 0,
		})
	}

	sortDiscoveryItems(items)
	return items
}

// AnalyzeBrowse lists TMDB movies in a release-year window (no actor or studio), optionally filtered by min vote average.
// At least one of minYear/maxYear must be set; missing bound defaults to 1900 or the current year.
func AnalyzeBrowse(ctx context.Context, cfg Config, _ *PlexClient, plexMovies []Movie, minYear, maxYear int, minVoteAverage float64, genreFilterIDs []int) ([]DiscoveryItem, string, error) {
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, "", fmt.Errorf("TMDB API key not configured")
	}
	nowY := time.Now().Year()
	if minYear <= 0 && maxYear <= 0 {
		return nil, "", fmt.Errorf("set at least one release year (min and/or max) for TMDB browse")
	}
	if minYear <= 0 {
		minYear = 1900
	}
	if maxYear <= 0 {
		maxYear = nowY
	}
	if minYear > maxYear {
		return nil, "", fmt.Errorf("min year cannot be after max year")
	}

	cache := newDiskDiscoveryCache(defaultDiscoveryCacheDir())
	genreFilterIDs = normalizeGenreIDs(genreFilterIDs)
	credits, err := loadBrowseDiscover(ctx, cfg.TMDBAPIKey, minYear, maxYear, genreFilterIDs, minVoteAverage, cache)
	if err != nil {
		return nil, "", err
	}
	label := fmt.Sprintf("%d–%d", minYear, maxYear)
	if gk := genreKeyFromIDs(genreFilterIDs); gk != "" {
		label += " · genres"
	}
	if v := discoveryEffectiveMinVote(minVoteAverage); v > 0 {
		label += fmt.Sprintf(" · ≥%.1f TMDB", v)
	}
	items := buildDiscoveryItemsFromTMDBDiscoverList(plexMovies, credits, label, minVoteAverage)
	items = applyOMDbBlendToDiscoveryItems(ctx, cfg, items, cache)
	items = filterDiscoveryItemsByMinVote(items, minVoteAverage)
	return items, label, nil
}

// AnalyzeStudio finds TMDB movies from a production company and cross-references
// them against the caller-supplied Plex library slice. This function never hits Plex.
func AnalyzeStudio(ctx context.Context, cfg Config, _ *PlexClient, plexMovies []Movie, companyName string, minYear, maxYear int, minVoteAverage float64, genreFilterIDs []int) ([]DiscoveryItem, string, error) {
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		return nil, "", fmt.Errorf("TMDB API key not configured")
	}
	companyName = strings.TrimSpace(companyName)
	if companyName == "" {
		return nil, "", fmt.Errorf("company name is required")
	}

	cache := newDiskDiscoveryCache(defaultDiscoveryCacheDir())

	// 1. Resolve company name → TMDB company ID (disk cache, same idea as person search).
	companyID, resolvedName, err := resolveCompanyID(ctx, cfg.TMDBAPIKey, companyName, cache)
	if err != nil {
		return nil, "", err
	}

	genreFilterIDs = normalizeGenreIDs(genreFilterIDs)
	// 2. Discover list for company + year range (disk cache; avoids paging TMDB every run).
	credits, err := loadStudioDiscover(ctx, cfg.TMDBAPIKey, companyID, minYear, maxYear, genreFilterIDs, minVoteAverage, cache)
	if err != nil {
		return nil, resolvedName, err
	}

	items := buildDiscoveryItemsFromTMDBDiscoverList(plexMovies, credits, resolvedName, minVoteAverage)
	items = applyOMDbBlendToDiscoveryItems(ctx, cfg, items, cache)
	items = filterDiscoveryItemsByMinVote(items, minVoteAverage)
	return items, resolvedName, nil
}

// tmdbDiscoverMovie is the raw result from /discover/movie.
type tmdbDiscoverMovie struct {
	TMDBID           int      `json:"id"`
	Title            string   `json:"title"`
	Year             int      `json:"year"` // parsed from release_date; stored in studio disk cache
	ReleaseDate      string   `json:"release_date"`
	Overview         string   `json:"overview"`
	VoteAverage      float64  `json:"vote_average"`
	PosterPath       string   `json:"poster_path"`
	OriginalLanguage string   `json:"original_language"`
	Runtime          int      `json:"runtime,omitempty"`
	Genres           []string `json:"genres"` // genre names resolved from genre_ids
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

func resolveCompanyID(ctx context.Context, apiKey, companyName string, cache *diskDiscoveryCache) (int, string, error) {
	if id, name, ok := cache.getCompanyID(companyName); ok {
		return id, name, nil
	}
	id, name, err := tmdbFindCompanyID(ctx, apiKey, companyName)
	if err != nil {
		return 0, "", err
	}
	cache.putCompanyID(companyName, id, name)
	return id, name, nil
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

func tmdbGenreMapWithCache(ctx context.Context, apiKey string, cache *diskDiscoveryCache) (map[int]string, error) {
	if m, ok := cache.getGenreMapMovie(); ok {
		return m, nil
	}
	m, err := tmdbGenreMap(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	cache.putGenreMapMovie(m)
	return m, nil
}

func loadBrowseDiscover(ctx context.Context, apiKey string, minYear, maxYear int, genreFilterIDs []int, minVoteAverage float64, cache *diskDiscoveryCache) ([]tmdbDiscoverMovie, error) {
	gk := genreKeyFromIDs(genreFilterIDs)
	extra := browseStudioDiscoverExtraKey(gk, minVoteAverage)
	if credits, ok := cache.getBrowseDiscover(minYear, maxYear, extra); ok {
		return credits, nil
	}
	genreMap, err := tmdbGenreMapWithCache(ctx, apiKey, cache)
	if err != nil {
		genreMap = map[int]string{}
	}
	credits, err := tmdbDiscoverBrowseWithGenreMap(ctx, apiKey, minYear, maxYear, genreFilterIDs, minVoteAverage, genreMap)
	if err != nil {
		return nil, err
	}
	cache.putBrowseDiscover(minYear, maxYear, extra, credits)
	return credits, nil
}

func loadStudioDiscover(ctx context.Context, apiKey string, companyID, minYear, maxYear int, genreFilterIDs []int, minVoteAverage float64, cache *diskDiscoveryCache) ([]tmdbDiscoverMovie, error) {
	gk := genreKeyFromIDs(genreFilterIDs)
	extra := browseStudioDiscoverExtraKey(gk, minVoteAverage)
	if credits, ok := cache.getStudioDiscover(companyID, minYear, maxYear, extra); ok {
		return credits, nil
	}
	genreMap, err := tmdbGenreMapWithCache(ctx, apiKey, cache)
	if err != nil {
		genreMap = map[int]string{}
	}
	credits, err := tmdbDiscoverByCompanyWithGenreMap(ctx, apiKey, companyID, minYear, maxYear, genreFilterIDs, minVoteAverage, genreMap)
	if err != nil {
		return nil, err
	}
	cache.putStudioDiscover(companyID, minYear, maxYear, extra, credits)
	return credits, nil
}

// tmdbDiscoverBrowseWithGenreMap pages through /discover/movie for a release-year window only (no studio filter).
// with_original_language=en matches our post-filters. When minVoteAverage > 0, vote_average.gte and sort_by=vote_average.desc
// are sent so TMDB returns high-rated rows (popularity-sorted pages would nearly all fail a strict min-rating filter).
func tmdbDiscoverBrowseWithGenreMap(ctx context.Context, apiKey string, minYear, maxYear int, genreFilterIDs []int, minVoteAverage float64, genreMap map[int]string) ([]tmdbDiscoverMovie, error) {
	if genreMap == nil {
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

	sortBy := "popularity.desc"
	if discoveryEffectiveMinVote(minVoteAverage) > 0 {
		sortBy = "vote_average.desc"
	}

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		u := fmt.Sprintf(
			"https://api.themoviedb.org/3/discover/movie?api_key=%s&sort_by=%s&with_original_language=en&vote_count.gte=15&page=%d",
			url.QueryEscape(apiKey), url.QueryEscape(sortBy), pageNum,
		)
		u += fmt.Sprintf("&primary_release_date.gte=%d-01-01", minYear)
		u += fmt.Sprintf("&primary_release_date.lte=%d-12-31", maxYear)
		u += tmdbDiscoverWithGenresQuery(genreFilterIDs)
		u += tmdbDiscoverVoteAverageGteQuery(minVoteAverage)

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

// tmdbDiscoverByCompanyWithGenreMap pages through /discover/movie for a given company ID.
// Up to 10 pages (~200 movies). Uses vote_average.desc + vote_average.gte when a min rating is set.
func tmdbDiscoverByCompanyWithGenreMap(ctx context.Context, apiKey string, companyID, minYear, maxYear int, genreFilterIDs []int, minVoteAverage float64, genreMap map[int]string) ([]tmdbDiscoverMovie, error) {
	if genreMap == nil {
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

	sortBy := "vote_count.desc"
	if discoveryEffectiveMinVote(minVoteAverage) > 0 {
		sortBy = "vote_average.desc"
	}

	for pageNum := 1; pageNum <= maxPages; pageNum++ {
		u := fmt.Sprintf(
			"https://api.themoviedb.org/3/discover/movie?api_key=%s&with_companies=%d&sort_by=%s&page=%d",
			url.QueryEscape(apiKey), companyID, url.QueryEscape(sortBy), pageNum,
		)
		if minYear > 0 {
			u += fmt.Sprintf("&primary_release_date.gte=%d-01-01", minYear)
		}
		if maxYear > 0 {
			u += fmt.Sprintf("&primary_release_date.lte=%d-12-31", maxYear)
		}
		u += tmdbDiscoverWithGenresQuery(genreFilterIDs)
		u += tmdbDiscoverVoteAverageGteQuery(minVoteAverage)

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
