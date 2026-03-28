package plexdash

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PlexClient struct {
	baseURL       string
	token         string
	client        *http.Client
	lgtvAddr      string // LG TV local IP for SSAP direct control
	lgtvClientKey string // SSAP client key (from one-time pairing)
}

type Movie struct {
	RatingKey         string
	Title             string
	Year              int
	TMDBID            int // from Plex guid when available; 0 if unknown
	IMDbID            string // from Plex guid when agent exposes imdb:// (normalized tt…)
	DurationMillis    int64
	LastViewedAtEpoch int64
	ViewCount         int
	Rating            float64
	Summary           string
	Studio            string
	Actors            []string
	Directors         []string
	Genres            []string
	PartKey           string // e.g. /library/parts/12345/file.mp4
	FileContainer     string // e.g. mp4, mkv
	PartSize          int64
}

type CreatePlaylistResult struct {
	Title          string
	Count          int
	FirstRatingKey string
}

type CreateAndPlayResult struct {
	PlaylistTitle string
	PlaylistCount int
	TargetClient  string
	PlaybackKey   string
	// SentTitles is set when playback is pushed to LG webOS as a native queue (direct URLs).
	SentTitles []string `json:"sentTitles,omitempty"`
}

// PlaybackSession is one row from GET /status/sessions (Plex-reported playback).
type PlaybackSession struct {
	PlayerName       string `json:"playerName"`
	PlayerProduct    string `json:"playerProduct"`
	PlayerState      string `json:"playerState"`
	MachineID        string `json:"machineId"`
	Title            string `json:"title"`
	GrandparentTitle string `json:"grandparentTitle,omitempty"`
	ParentTitle      string `json:"parentTitle,omitempty"`
	Type             string `json:"type"`
	Year             string `json:"year,omitempty"`
	ViewOffsetMs     int64  `json:"viewOffsetMs"`
	DurationMs       int64  `json:"durationMs"`
	ProgressPercent  float64 `json:"progressPercent"`
}

// DisplayTitle returns a human-readable primary title for the session.
func (p PlaybackSession) DisplayTitle() string {
	t := strings.TrimSpace(p.Type)
	if t == "episode" && strings.TrimSpace(p.GrandparentTitle) != "" {
		if strings.TrimSpace(p.ParentTitle) != "" {
			return fmt.Sprintf("%s — %s — %s", p.GrandparentTitle, p.ParentTitle, p.Title)
		}
		return fmt.Sprintf("%s — %s", p.GrandparentTitle, p.Title)
	}
	if strings.TrimSpace(p.Year) != "" {
		return fmt.Sprintf("%s (%s)", p.Title, p.Year)
	}
	return p.Title
}

type Player struct {
	Name             string
	ClientIdentifier string
	Product          string
	URI              string
}

// PlayerDiscoveryDebug explains how the merged player list was built (for UI / troubleshooting).
type PlayerDiscoveryDebug struct {
	LGStaticConfigured bool `json:"lgStaticConfigured"`

	CloudResourcesOK  bool   `json:"cloudResourcesOk"`
	CloudResourcesErr string `json:"cloudResourcesErr,omitempty"`
	CloudPlayersAdded int    `json:"cloudPlayersAdded"` // devices with Provides=player merged from plex.tv

	SessionDiscoveryOK  bool   `json:"sessionDiscoveryOk"`
	SessionDiscoveryErr string `json:"sessionDiscoveryErr,omitempty"`
	ActiveSessionCount  int    `json:"activeSessionCount"` // rows from /status/sessions

	MergedTotal int `json:"mergedTotal"`
}

type Playlist struct {
	RatingKey string
	Title     string
}

func NewPlexClient(cfg Config) *PlexClient {
	return &PlexClient{
		baseURL:       cfg.PlexBaseURL,
		token:         cfg.PlexToken,
		lgtvAddr:      cfg.LGTVAddr,
		lgtvClientKey: cfg.LGTVClientKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *PlexClient) IsConfigured() bool {
	return strings.TrimSpace(p.baseURL) != "" && strings.TrimSpace(p.token) != ""
}

// plexExternalIDsFromVideo returns TMDB / IMDb ids from the Video guid attribute
// and from child <Guid id="tmdb://…"/> / <Guid id="imdb://…"/> (present when Plex
// was queried with includeGuids=1). Modern Plex uses plex://movie/… on the attribute
// and puts provider ids only in those children.
func plexExternalIDsFromVideo(v video) (tmdbID int, imdbID string) {
	sources := []string{strings.TrimSpace(v.Guid)}
	for _, g := range v.GuidElems {
		sources = append(sources, strings.TrimSpace(g.ID))
	}
	for _, s := range sources {
		if s == "" {
			continue
		}
		if tmdbID == 0 {
			if x := parseTMDBIDFromPlexGuid(s); x > 0 {
				tmdbID = x
			}
		}
		if imdbID == "" {
			if x := parseIMDbIDFromPlexGuid(s); x != "" {
				imdbID = x
			}
		}
	}
	return tmdbID, imdbID
}

func movieFromVideo(video video) Movie {
	year, _ := strconv.Atoi(video.Year)
	duration, _ := strconv.ParseInt(video.Duration, 10, 64)
	lastViewedAt, _ := strconv.ParseInt(video.LastViewedAt, 10, 64)
	rating, _ := strconv.ParseFloat(video.Rating, 64)
	viewCount, _ := strconv.Atoi(video.ViewCount)
	tmdbID, imdbID := plexExternalIDsFromVideo(video)

	// Pick the media version with the largest file — Plex can store
	// multiple quality versions (e.g. 4K + 1080p) under one entry.
	var partKey, fileContainer string
	var partSize int64
	for _, media := range video.Medias {
		if len(media.Parts) == 0 {
			continue
		}
		sz, _ := strconv.ParseInt(media.Parts[0].Size, 10, 64)
		if sz > partSize {
			partSize = sz
			partKey = media.Parts[0].Key
			fileContainer = media.Container
		}
	}

	return Movie{
		RatingKey:         video.RatingKey,
		Title:             video.Title,
		Year:              year,
		TMDBID:            tmdbID,
		IMDbID:            imdbID,
		DurationMillis:    duration,
		LastViewedAtEpoch: lastViewedAt,
		ViewCount:         viewCount,
		Rating:            rating,
		Summary:           video.Summary,
		Studio:            video.Studio,
		Actors:            tagsToStrings(video.Roles),
		Directors:         tagsToStrings(video.Directors),
		Genres:            tagsToStrings(video.Genres),
		PartKey:           partKey,
		FileContainer:     fileContainer,
		PartSize:          partSize,
	}
}

func (p *PlexClient) ListMovies(ctx context.Context, libraryKey string) ([]Movie, error) {
	// includeGuids=1 adds <Guid id="tmdb://…"/> children; without it guid is often only
	// plex://movie/… and Discovery cannot match by TMDB id.
	endpoint := fmt.Sprintf("%s/library/sections/%s/all?includeGuids=1", p.baseURL, libraryKey)
	body, err := p.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var root mediaContainer
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode movie list: %w", err)
	}

	movies := make([]Movie, 0, len(root.Videos))
	for _, v := range root.Videos {
		movies = append(movies, movieFromVideo(v))
	}

	SortMoviesDefaultView(movies)

	return movies, nil
}

// SortMoviesDefaultView orders movies for the main dashboard: newest release year
// first (scrolling goes back in time), then higher Plex rating, then a random
// tiebreak so ties are not strictly alphabetical.
func SortMoviesDefaultView(movies []Movie) {
	SortMoviesDefaultViewWithRand(movies, rand.New(rand.NewSource(time.Now().UnixNano())))
}

// SortMoviesDefaultViewWithRand is the testable variant (fixed seed ⇒ deterministic ties).
func SortMoviesDefaultViewWithRand(movies []Movie, rng *rand.Rand) {
	if len(movies) < 2 || rng == nil {
		return
	}
	tie := make([]float64, len(movies))
	for i := range movies {
		tie[i] = rng.Float64()
	}
	sort.SliceStable(movies, func(i, j int) bool {
		ai, bi := movies[i], movies[j]
		if ai.Year != bi.Year {
			return ai.Year > bi.Year
		}
		if ai.Rating != bi.Rating {
			return ai.Rating > bi.Rating
		}
		return tie[i] < tie[j]
	})
}

// FetchNewMoviesFromRecentlyAdded walks /library/sections/{key}/recentlyAdded in
// pages (newest first) and returns Movie rows whose ratingKey is not in existing.
// Stops once it has collected at least wantNew items, or there are no more pages.
// This is cheaper than ListMovies when only a few titles were added.
func (p *PlexClient) FetchNewMoviesFromRecentlyAdded(ctx context.Context, libraryKey string, existing map[string]struct{}, wantNew int) ([]Movie, error) {
	if wantNew <= 0 {
		return nil, nil
	}
	const pageSize = 200
	var collected []Movie
	start := 0
	for page := 0; page < 50; page++ {
		endpoint := fmt.Sprintf("%s/library/sections/%s/recentlyAdded?X-Plex-Container-Start=%d&X-Plex-Container-Size=%d&includeGuids=1", p.baseURL, libraryKey, start, pageSize)
		body, err := p.get(ctx, endpoint)
		if err != nil {
			return collected, err
		}
		var root mediaContainer
		if err := xml.Unmarshal(body, &root); err != nil {
			return collected, fmt.Errorf("decode recently added: %w", err)
		}
		if len(root.Videos) == 0 {
			break
		}
		for _, v := range root.Videos {
			rk := v.RatingKey
			if rk == "" {
				continue
			}
			if _, ok := existing[rk]; ok {
				continue
			}
			existing[rk] = struct{}{}
			collected = append(collected, movieFromVideo(v))
			if len(collected) >= wantNew {
				return collected, nil
			}
		}
		if len(root.Videos) < pageSize {
			break
		}
		start += pageSize
	}
	return collected, nil
}

// errPlexTotalSizeMissing is returned when Plex omits totalSize on a paginated
// /all response (older servers); a full ListMovies is then required for count.
var errPlexTotalSizeMissing = errors.New("plex MediaContainer missing totalSize on paginated response")

// LibraryMovieTotalCount asks Plex for the movie count in a library section
// using one paginated request (one Video in the body plus MediaContainer attrs).
// This avoids downloading the full library metadata.
func (p *PlexClient) LibraryMovieTotalCount(ctx context.Context, libraryKey string) (int, error) {
	endpoint := fmt.Sprintf("%s/library/sections/%s/all?X-Plex-Container-Start=0&X-Plex-Container-Size=1", p.baseURL, libraryKey)
	body, err := p.get(ctx, endpoint)
	if err != nil {
		return 0, err
	}
	return movieCountFromPlexLibraryAllXML(body)
}

func movieCountFromPlexLibraryAllXML(body []byte) (int, error) {
	var mc struct {
		XMLName   xml.Name `xml:"MediaContainer"`
		Size      string   `xml:"size,attr"`
		TotalSize string   `xml:"totalSize,attr"`
		Videos    []struct {
			XMLName xml.Name `xml:"Video"`
		} `xml:"Video"`
	}
	if err := xml.Unmarshal(body, &mc); err != nil {
		return 0, fmt.Errorf("decode media container: %w", err)
	}
	if ts := strings.TrimSpace(mc.TotalSize); ts != "" {
		n, err := strconv.Atoi(ts)
		if err != nil {
			return 0, fmt.Errorf("parse totalSize: %w", err)
		}
		if n < 0 {
			return 0, fmt.Errorf("negative totalSize")
		}
		return n, nil
	}
	if sz := strings.TrimSpace(mc.Size); sz != "" {
		n, err := strconv.Atoi(sz)
		if err != nil {
			return 0, fmt.Errorf("parse size: %w", err)
		}
		if n < 0 {
			return 0, fmt.Errorf("negative size")
		}
		// Paginated: size is the page length (often 1), not the library total.
		if len(mc.Videos) == 1 && n == 1 {
			return 0, errPlexTotalSizeMissing
		}
		return n, nil
	}
	return 0, fmt.Errorf("no size attributes in Plex MediaContainer")
}

func (p *PlexClient) CreateRandomPlaylist(ctx context.Context, libraryKey, title string, count int) (CreatePlaylistResult, error) {
	movies, err := p.ListMovies(ctx, libraryKey)
	if err != nil {
		return CreatePlaylistResult{}, err
	}
	if len(movies) == 0 {
		return CreatePlaylistResult{}, fmt.Errorf("no movies found in library section %s", libraryKey)
	}
	if count <= 0 {
		count = 10
	}
	if count > len(movies) {
		count = len(movies)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ordered := ratingKeysOrderedByViewCountShuffleTies(movies, rng)
	ratingKeys := ordered[:count]
	result, err := p.createPlaylistFromRatingKeys(ctx, title, ratingKeys)
	if err != nil {
		return CreatePlaylistResult{}, err
	}
	result.FirstRatingKey = ratingKeys[0]
	return result, nil
}

func (p *PlexClient) CreatePlaylistByPeople(ctx context.Context, libraryKey, title, actor, director string, count int) (CreatePlaylistResult, error) {
	if strings.TrimSpace(actor) == "" && strings.TrimSpace(director) == "" {
		return CreatePlaylistResult{}, fmt.Errorf("actor or director is required")
	}

	movies, err := p.ListMovies(ctx, libraryKey)
	if err != nil {
		return CreatePlaylistResult{}, err
	}

	filtered := filterMoviesByPeople(movies, actor, director)
	if len(filtered) == 0 {
		return CreatePlaylistResult{}, fmt.Errorf("no movies matched actor=%q director=%q", actor, director)
	}

	if count <= 0 {
		count = 10
	}
	if count > len(filtered) {
		count = len(filtered)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ordered := ratingKeysOrderedByViewCountShuffleTies(filtered, rng)
	ratingKeys := ordered[:count]

	result, err := p.createPlaylistFromRatingKeys(ctx, title, ratingKeys)
	if err != nil {
		return CreatePlaylistResult{}, err
	}
	result.FirstRatingKey = ratingKeys[0]
	return result, nil
}

func (p *PlexClient) CreatePlaylistByGenreRatingYear(ctx context.Context, libraryKey, genre string, minRating float64, minYear, maxYear int) (CreatePlaylistResult, error) {
	movies, err := p.ListMovies(ctx, libraryKey)
	if err != nil {
		return CreatePlaylistResult{}, err
	}

	filtered := filterMoviesByGenreRatingYear(movies, genre, minRating, minYear, maxYear)
	if len(filtered) == 0 {
		return CreatePlaylistResult{}, fmt.Errorf("no movies matched genre=%q rating>=%.1f years=%d-%d", genre, minRating, minYear, maxYear)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ratingKeys := ratingKeysOrderedByViewCountShuffleTies(filtered, rng)

	title := strings.ToUpper(strings.TrimSpace(genre))
	if title == "" {
		title = "ALL"
	}
	title += "-rando"

	result, err := p.createPlaylistFromRatingKeys(ctx, title, ratingKeys)
	if err != nil {
		return CreatePlaylistResult{}, err
	}
	result.FirstRatingKey = ratingKeys[0]
	return result, nil
}

func (p *PlexClient) ListGenres(ctx context.Context, libraryKey string) ([]string, error) {
	movies, err := p.ListMovies(ctx, libraryKey)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	genres := make([]string, 0)
	for _, movie := range movies {
		for _, genre := range movie.Genres {
			normalized := strings.TrimSpace(genre)
			if normalized == "" {
				continue
			}
			key := strings.ToLower(normalized)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			genres = append(genres, normalized)
		}
	}

	sort.SliceStable(genres, func(i, j int) bool {
		return strings.ToLower(genres[i]) < strings.ToLower(genres[j])
	})
	return genres, nil
}

// ListPlayersResult is the merged player list plus diagnostics for troubleshooting empty lists.
type ListPlayersResult struct {
	Players []Player
	Debug   PlayerDiscoveryDebug
}

// ListPlayers finds Plex players in up to three places:
//  1. Static config — if LGTV_ADDR is set, the LG TV is always present.
//  2. plex.tv/api/resources — desktop/registered players (direct URI).
//  3. /status/sessions on the local server — actively-streaming players
//     that didn't register with plex.tv (LG, Samsung, etc. embedded apps).
func (p *PlexClient) ListPlayers(ctx context.Context) ListPlayersResult {
	var dbg PlayerDiscoveryDebug
	seen := map[string]Player{} // keyed by ClientIdentifier

	// Source 1: static LG TV from config (always available, no session needed).
	if p.lgtvAddr != "" && p.lgtvClientKey != "" {
		dbg.LGStaticConfigured = true
		const lgtvStaticID = "lgtv-ssap-static"
		seen[lgtvStaticID] = Player{
			Name:             "LG TV (SSAP)",
			ClientIdentifier: lgtvStaticID,
			Product:          "Plex for LG",
			URI:              "ssap://" + p.lgtvAddr,
		}
	}

	// Source 2: cloud-registered resources.
	if resources, err := p.listResources(ctx); err == nil {
		dbg.CloudResourcesOK = true
		for _, device := range resources.Devices {
			if !strings.Contains(device.Provides, "player") || len(device.Connections) == 0 {
				continue
			}
			dbg.CloudPlayersAdded++
			seen[device.ClientIdentifier] = Player{
				Name:             device.Name,
				ClientIdentifier: device.ClientIdentifier,
				Product:          device.Product,
				URI:              device.Connections[0].URI,
			}
		}
	} else {
		dbg.CloudResourcesErr = err.Error()
		fmt.Printf("[players] cloud resources unavailable: %v\n", err)
	}

	// Source 3: active streaming sessions (enriches with live machine ID).
	if sessionPlayers, err := p.listSessionPlayers(ctx); err == nil {
		dbg.SessionDiscoveryOK = true
		dbg.ActiveSessionCount = len(sessionPlayers)
		for _, sp := range sessionPlayers {
			if _, exists := seen[sp.ClientIdentifier]; !exists {
				seen[sp.ClientIdentifier] = sp
			}
			// If this session player is the configured LG TV, update the static
			// entry with the live machine identifier from the session.
			if strings.EqualFold(sp.Product, "Plex for LG") && p.lgtvAddr != "" {
				const lgtvStaticID = "lgtv-ssap-static"
				if s, ok := seen[lgtvStaticID]; ok {
					s.Name = sp.Name
					seen[lgtvStaticID] = s
				}
			}
		}
	} else {
		dbg.SessionDiscoveryErr = err.Error()
		fmt.Printf("[players] session discovery unavailable: %v\n", err)
	}

	players := make([]Player, 0, len(seen))
	for _, pl := range seen {
		players = append(players, pl)
	}
	sort.SliceStable(players, func(i, j int) bool {
		return strings.ToLower(players[i].Name) < strings.ToLower(players[j].Name)
	})
	dbg.MergedTotal = len(players)
	return ListPlayersResult{Players: players, Debug: dbg}
}

// listSessionPlayers reads /status/sessions on the Plex server and returns a
// Player for each active streaming client. Commands to these players are sent
// via the plex.tv relay (URI = "https://plex.tv") since embedded TV apps
// (webOS, Tizen, etc.) are not reachable on the LAN directly.
func (p *PlexClient) listSessionPlayers(ctx context.Context) ([]Player, error) {
	body, err := p.get(ctx, p.baseURL+"/status/sessions")
	if err != nil {
		return nil, err
	}
	var root struct {
		XMLName xml.Name `xml:"MediaContainer"`
		Videos  []struct {
			Player struct {
				MachineIdentifier string `xml:"machineIdentifier,attr"`
				Title             string `xml:"title,attr"`
				Product           string `xml:"product,attr"`
				State             string `xml:"state,attr"`
			} `xml:"Player"`
		} `xml:"Video"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}
	players := make([]Player, 0, len(root.Videos))
	for _, v := range root.Videos {
		pl := v.Player
		if pl.MachineIdentifier == "" {
			continue
		}
		players = append(players, Player{
			Name:             pl.Title,
			ClientIdentifier: pl.MachineIdentifier,
			Product:          pl.Product,
			URI:              "https://plex.tv", // relay — no direct LAN access needed
		})
	}
	return players, nil
}

// ListPlaybackSessions returns Plex-reported playback rows from GET /status/sessions.
// Note: LG webOS direct streaming often does not create a Plex session; use server-side
// "last sent" tracking in that case.
func (p *PlexClient) ListPlaybackSessions(ctx context.Context) ([]PlaybackSession, error) {
	body, err := p.get(ctx, p.baseURL+"/status/sessions")
	if err != nil {
		return nil, err
	}
	var root struct {
		XMLName xml.Name `xml:"MediaContainer"`
		Videos  []struct {
			Type             string `xml:"type,attr"`
			Title            string `xml:"title,attr"`
			GrandparentTitle string `xml:"grandparentTitle,attr"`
			ParentTitle      string `xml:"parentTitle,attr"`
			Year             string `xml:"year,attr"`
			ViewOffset       string `xml:"viewOffset,attr"`
			Duration         string `xml:"duration,attr"`
			Player           struct {
				MachineIdentifier string `xml:"machineIdentifier,attr"`
				Title             string `xml:"title,attr"`
				Product           string `xml:"product,attr"`
				State             string `xml:"state,attr"`
			} `xml:"Player"`
		} `xml:"Video"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}
	out := make([]PlaybackSession, 0, len(root.Videos))
	for _, v := range root.Videos {
		pl := v.Player
		if pl.MachineIdentifier == "" {
			continue
		}
		offset, _ := strconv.ParseInt(v.ViewOffset, 10, 64)
		dur, _ := strconv.ParseInt(v.Duration, 10, 64)
		var pct float64
		if dur > 0 {
			pct = 100 * float64(offset) / float64(dur)
		}
		out = append(out, PlaybackSession{
			PlayerName:       pl.Title,
			PlayerProduct:    pl.Product,
			PlayerState:      pl.State,
			MachineID:        pl.MachineIdentifier,
			Title:            v.Title,
			GrandparentTitle: v.GrandparentTitle,
			ParentTitle:      v.ParentTitle,
			Type:             v.Type,
			Year:             v.Year,
			ViewOffsetMs:     offset,
			DurationMs:       dur,
			ProgressPercent:  pct,
		})
	}
	return out, nil
}

func (p *PlexClient) ListPlaylists(ctx context.Context) ([]Playlist, error) {
	body, err := p.get(ctx, p.baseURL+"/playlists")
	if err != nil {
		return nil, err
	}
	var root playlistsContainer
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode playlists: %w", err)
	}
	playlists := make([]Playlist, 0, len(root.Playlists))
	for _, item := range root.Playlists {
		playlists = append(playlists, Playlist{RatingKey: item.RatingKey, Title: item.Title})
	}
	return playlists, nil
}

func (p *PlexClient) PlaylistMovies(ctx context.Context, playlistTitle string, limit int) ([]Movie, error) {
	playlists, err := p.ListPlaylists(ctx)
	if err != nil {
		return nil, err
	}

	var playlistID string
	target := strings.ToLower(strings.TrimSpace(playlistTitle))
	for _, playlist := range playlists {
		if strings.ToLower(strings.TrimSpace(playlist.Title)) == target {
			playlistID = playlist.RatingKey
			break
		}
	}
	if playlistID == "" {
		return nil, fmt.Errorf("playlist %q not found", playlistTitle)
	}

	body, err := p.get(ctx, fmt.Sprintf("%s/playlists/%s/items", p.baseURL, playlistID))
	if err != nil {
		return nil, err
	}

	var root mediaContainer
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode playlist items: %w", err)
	}

	movies := make([]Movie, 0, len(root.Videos))
	for _, video := range root.Videos {
		year, _ := strconv.Atoi(video.Year)
		duration, _ := strconv.ParseInt(video.Duration, 10, 64)
		lastViewedAt, _ := strconv.ParseInt(video.LastViewedAt, 10, 64)
		rating, _ := strconv.ParseFloat(video.Rating, 64)
		viewCount, _ := strconv.Atoi(video.ViewCount)

		movies = append(movies, Movie{
			RatingKey:         video.RatingKey,
			Title:             video.Title,
			Year:              year,
			DurationMillis:    duration,
			LastViewedAtEpoch: lastViewedAt,
			ViewCount:         viewCount,
			Rating:            rating,
			Actors:            tagsToStrings(video.Roles),
			Directors:         tagsToStrings(video.Directors),
			Genres:            tagsToStrings(video.Genres),
		})
	}

	if limit > 0 && limit < len(movies) {
		return movies[:limit], nil
	}
	return movies, nil
}

func (p *PlexClient) PlaylistMovieTitles(ctx context.Context, playlistTitle string) (map[string]struct{}, error) {
	playlists, err := p.ListPlaylists(ctx)
	if err != nil {
		return nil, err
	}
	var playlistID string
	target := strings.ToLower(strings.TrimSpace(playlistTitle))
	for _, playlist := range playlists {
		if strings.ToLower(strings.TrimSpace(playlist.Title)) == target {
			playlistID = playlist.RatingKey
			break
		}
	}
	if playlistID == "" {
		return map[string]struct{}{}, nil
	}

	body, err := p.get(ctx, fmt.Sprintf("%s/playlists/%s/items", p.baseURL, playlistID))
	if err != nil {
		return nil, err
	}
	var root mediaContainer
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode playlist items: %w", err)
	}
	titles := map[string]struct{}{}
	for _, item := range root.Videos {
		key := normalizeTitleYear(item.Title, item.Year)
		if key != "" {
			titles[key] = struct{}{}
		}
	}
	return titles, nil
}

func (p *PlexClient) PlayPlaylistOnClient(ctx context.Context, playlistTitle, targetClientName string) (CreateAndPlayResult, error) {
	playlists, err := p.ListPlaylists(ctx)
	if err != nil {
		return CreateAndPlayResult{}, err
	}
	target2 := strings.ToLower(strings.TrimSpace(playlistTitle))
	var playlistID string
	for _, pl := range playlists {
		if strings.ToLower(strings.TrimSpace(pl.Title)) == target2 {
			playlistID = pl.RatingKey
			break
		}
	}
	if playlistID == "" {
		return CreateAndPlayResult{}, fmt.Errorf("playlist %q not found", playlistTitle)
	}

	lp := p.ListPlayers(ctx)
	target, err := selectPlayer(lp.Players, targetClientName)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	// For the LG TV, stream directly through the webOS native media player
	// using direct Plex HTTP URLs — the Plex companion protocol is not supported.
	if strings.EqualFold(target.Product, "Plex for LG") && p.lgtvAddr != "" && p.lgtvClientKey != "" {
		streamItems, err := p.fetchPlaylistStreamItems(ctx, playlistID)
		if err != nil {
			return CreateAndPlayResult{}, fmt.Errorf("fetch stream items: %w", err)
		}
		fmt.Printf("[player] LG TV: streaming %d items via webOS native player\n", len(streamItems))
		if err := PlayPlaylistViaWebOS(ctx, p.lgtvAddr, p.lgtvClientKey, streamItems); err != nil {
			return CreateAndPlayResult{}, err
		}
		sentTitles := make([]string, len(streamItems))
		for i := range streamItems {
			sentTitles[i] = strings.TrimSpace(streamItems[i].Title)
		}
		return CreateAndPlayResult{
			PlaylistTitle: playlistTitle,
			PlaylistCount: len(streamItems),
			TargetClient:  target.Name,
			PlaybackKey:   "",
			SentTitles:    sentTitles,
		}, nil
	}

	items, err := p.PlaylistMovies(ctx, playlistTitle, 1)
	if err != nil {
		return CreateAndPlayResult{}, err
	}
	if len(items) == 0 {
		return CreateAndPlayResult{}, fmt.Errorf("playlist %q has no items", playlistTitle)
	}

	containerKey := "/playlists/" + playlistID + "/items"
	if err := p.sendPlayCommand(ctx, target, items[0].RatingKey, containerKey); err != nil {
		return CreateAndPlayResult{}, err
	}

	return CreateAndPlayResult{
		PlaylistTitle: playlistTitle,
		PlaylistCount: 0,
		TargetClient:  target.Name,
		PlaybackKey:   items[0].RatingKey,
	}, nil
}

// fetchPlaylistStreamItems returns direct HTTP stream items for all videos in a
// Plex playlist, shuffled into a random order, suitable for the webOS native media player.
func (p *PlexClient) fetchPlaylistStreamItems(ctx context.Context, playlistID string) ([]WebOSStreamItem, error) {
	body, err := p.get(ctx, fmt.Sprintf("%s/playlists/%s/items", p.baseURL, playlistID))
	if err != nil {
		return nil, err
	}
	var root mediaContainer
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode playlist items: %w", err)
	}

	items := make([]WebOSStreamItem, 0, len(root.Videos))
	for _, v := range root.Videos {
		if len(v.Medias) == 0 || len(v.Medias[0].Parts) == 0 {
			continue
		}
		part := v.Medias[0].Parts[0]
		container := v.Medias[0].Container
		size, _ := strconv.ParseInt(part.Size, 10, 64)
		title := v.Title
		if v.Year != "" {
			title = fmt.Sprintf("%s (%s)", v.Title, v.Year)
		}
		items = append(items, WebOSStreamItem{
			StreamURL: p.baseURL + part.Key + "?X-Plex-Token=" + p.token,
			Title:     title,
			Container: container,
			Size:      size,
		})
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	return items, nil
}

// PlayStreamItemsOnTV routes a pre-built list of WebOSStreamItems to the
// appropriate player. For the LG TV it uses the webOS native media player;
// the companion protocol path is not supported for raw stream URLs.
func (p *PlexClient) PlayStreamItemsOnTV(ctx context.Context, items []WebOSStreamItem, targetClientName string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to play")
	}
	lp := p.ListPlayers(ctx)
	clientName := strings.TrimSpace(targetClientName)
	if clientName == "" {
		clientName = "Living Room"
	}
	target, err := selectPlayer(lp.Players, clientName)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(target.Product, "Plex for LG") && p.lgtvAddr != "" && p.lgtvClientKey != "" {
		fmt.Printf("[player] LG TV: streaming %d direct items via webOS native player\n", len(items))
		return target.Name, PlayPlaylistViaWebOS(ctx, p.lgtvAddr, p.lgtvClientKey, items)
	}
	return target.Name, fmt.Errorf("direct stream playback only supported on LG webOS target")
}

func (p *PlexClient) CreateRandomPlaylistAndPlay(ctx context.Context, libraryKey, playlistTitle string, count int, targetClientName string) (CreateAndPlayResult, error) {
	created, err := p.CreateRandomPlaylist(ctx, libraryKey, playlistTitle, count)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	lp := p.ListPlayers(ctx)
	target, err := selectPlayer(lp.Players, targetClientName)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	if err := p.sendPlayCommand(ctx, target, created.FirstRatingKey, "/library/metadata/"+created.FirstRatingKey); err != nil {
		return CreateAndPlayResult{}, err
	}

	return CreateAndPlayResult{
		PlaylistTitle: created.Title,
		PlaylistCount: created.Count,
		TargetClient:  target.Name,
		PlaybackKey:   created.FirstRatingKey,
	}, nil
}

func (p *PlexClient) createPlaylistFromRatingKeys(ctx context.Context, title string, ratingKeys []string) (CreatePlaylistResult, error) {
	if len(ratingKeys) == 0 {
		return CreatePlaylistResult{}, fmt.Errorf("rating keys are required")
	}

	machineIdentifier, err := p.MachineIdentifier(ctx)
	if err != nil {
		return CreatePlaylistResult{}, err
	}

	uri := fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s", machineIdentifier, strings.Join(ratingKeys, ","))
	endpoint := fmt.Sprintf("%s/playlists", p.baseURL)

	form := url.Values{}
	form.Set("type", "video")
	form.Set("title", title)
	form.Set("smart", "0")
	form.Set("uri", uri)

	if _, err := p.post(ctx, endpoint, form); err != nil {
		return CreatePlaylistResult{}, err
	}

	return CreatePlaylistResult{
		Title: title,
		Count: len(ratingKeys),
	}, nil
}

func (p *PlexClient) MachineIdentifier(ctx context.Context) (string, error) {
	body, err := p.get(ctx, p.baseURL+"/")
	if err != nil {
		return "", err
	}

	var root mediaContainerRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return "", fmt.Errorf("decode root response: %w", err)
	}
	if strings.TrimSpace(root.MachineIdentifier) == "" {
		return "", fmt.Errorf("machineIdentifier missing from Plex root response")
	}
	return root.MachineIdentifier, nil
}

// sendPlayCommand routes to the correct play mechanism based on the player type.
// For all known players it uses the standard Plex HTTP companion protocol.
// The LG TV path is handled earlier in PlayPlaylistOnClient via PlayPlaylistViaWebOS.
func (p *PlexClient) sendPlayCommand(ctx context.Context, player Player, ratingKey, containerKey string) error {
	return p.playOnClientWithContainer(ctx, player, ratingKey, containerKey)
}

// playOnClientWithContainer sends a playMedia command to the Plex player, using
// containerKey as the queue context. When containerKey is a playlist items path
// (e.g. /playlists/{id}/items), the player advances through all items automatically.
func (p *PlexClient) playOnClientWithContainer(ctx context.Context, player Player, ratingKey, containerKey string) error {
	serverMachineIdentifier, err := p.MachineIdentifier(ctx)
	if err != nil {
		return err
	}
	serverURL, err := url.Parse(p.baseURL)
	if err != nil {
		return fmt.Errorf("parse base server URL: %w", err)
	}
	playerURL, err := url.Parse(player.URI)
	if err != nil {
		return fmt.Errorf("parse player URI: %w", err)
	}

	q := url.Values{}
	q.Set("X-Plex-Token", p.token)
	q.Set("key", "/library/metadata/"+ratingKey)
	q.Set("offset", "0")
	q.Set("machineIdentifier", serverMachineIdentifier)
	q.Set("address", serverURL.Hostname())
	q.Set("port", serverURL.Port())
	q.Set("protocol", serverURL.Scheme)
	q.Set("containerKey", containerKey)
	q.Set("type", "video")
	q.Set("providerIdentifier", "com.plexapp.plugins.library")

	playerURL.Path = "/player/playback/playMedia"
	playerURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playerURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build play request: %w", err)
	}
	req.Header.Set("X-Plex-Client-Identifier", "plex-dashboard")
	req.Header.Set("X-Plex-Target-Client-Identifier", player.ClientIdentifier)
	req.Header.Set("X-Plex-Product", "plex-dashboard")
	req.Header.Set("X-Plex-Version", "0.1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send play command: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("play command failed for %q: status=%d body=%s", player.Name, resp.StatusCode, string(body))
	}
	return nil
}

func (p *PlexClient) playMovieOnClient(ctx context.Context, player Player, ratingKey string) error {
	serverMachineIdentifier, err := p.MachineIdentifier(ctx)
	if err != nil {
		return err
	}
	serverURL, err := url.Parse(p.baseURL)
	if err != nil {
		return fmt.Errorf("parse base server URL: %w", err)
	}
	playerURL, err := url.Parse(player.URI)
	if err != nil {
		return fmt.Errorf("parse player URI: %w", err)
	}

	q := url.Values{}
	q.Set("X-Plex-Token", p.token)
	q.Set("key", "/library/metadata/"+ratingKey)
	q.Set("offset", "0")
	q.Set("machineIdentifier", serverMachineIdentifier)
	q.Set("address", serverURL.Hostname())
	q.Set("port", serverURL.Port())
	q.Set("protocol", serverURL.Scheme)
	q.Set("containerKey", "/library/metadata/"+ratingKey)
	q.Set("type", "video")
	q.Set("providerIdentifier", "com.plexapp.plugins.library")

	playerURL.Path = "/player/playback/playMedia"
	playerURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playerURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build play request: %w", err)
	}
	req.Header.Set("X-Plex-Client-Identifier", "plex-dashboard")
	req.Header.Set("X-Plex-Target-Client-Identifier", player.ClientIdentifier)
	req.Header.Set("X-Plex-Product", "plex-dashboard")
	req.Header.Set("X-Plex-Version", "0.1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send play command: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("play command failed for %q: status=%d body=%s", player.Name, resp.StatusCode, string(body))
	}
	return nil
}

// PlayerUsesSSAP reports whether commands must go through LG SSAP (not Plex companion HTTP).
func PlayerUsesSSAP(p Player) bool {
	return strings.HasPrefix(strings.TrimSpace(p.URI), "ssap://")
}

// SelectPlayerForCompanion resolves a target name to a player with a Plex companion-capable
// URI (https://… or http://…). When both an SSAP row and a session/relay row exist for the
// same TV name, the non-SSAP entry is preferred.
func SelectPlayerForCompanion(players []Player, targetClientName string) (Player, error) {
	target := strings.TrimSpace(strings.ToLower(targetClientName))
	if target == "" {
		return Player{}, fmt.Errorf("target client name is required")
	}
	var matched []Player
	for _, pl := range players {
		if strings.Contains(strings.ToLower(pl.Name), target) {
			matched = append(matched, pl)
		}
	}
	for _, pl := range matched {
		if !PlayerUsesSSAP(pl) {
			return pl, nil
		}
	}
	if len(matched) > 0 {
		return Player{}, fmt.Errorf("only SSAP targets match %q; Plex companion needs a player with an HTTP URI — start playback on the TV once so it appears in server sessions, or pick another client", targetClientName)
	}
	pl, err := selectPlayer(players, targetClientName)
	if err != nil {
		return Player{}, err
	}
	if PlayerUsesSSAP(pl) {
		return Player{}, fmt.Errorf("player %q is SSAP-only; companion commands require a Plex player URL (not webOS direct)", pl.Name)
	}
	return pl, nil
}

// SendPlaybackControl sends a GET to the Plex companion playback API (same style as playMedia).
// action is the path segment after /player/playback/ (e.g. pause, play, skipNext, seekTo).
// For seekTo, seekOffsetMs is the offset in milliseconds; for other actions it is ignored.
func (p *PlexClient) SendPlaybackControl(ctx context.Context, player Player, action string, seekOffsetMs int64) error {
	if PlayerUsesSSAP(player) {
		return fmt.Errorf("companion playback control does not apply to SSAP player %q", player.Name)
	}
	action = strings.TrimSpace(strings.TrimPrefix(action, "/"))
	if action == "" {
		return fmt.Errorf("playback action is required")
	}
	allowed := map[string]struct{}{
		"pause":         {},
		"play":          {},
		"stop":          {},
		"skipNext":      {},
		"skipPrevious":  {},
		"stepForward":   {},
		"stepBack":      {},
		"seekTo":        {},
	}
	if _, ok := allowed[action]; !ok {
		return fmt.Errorf("unsupported playback action %q", action)
	}
	if action == "seekTo" && seekOffsetMs < 0 {
		return fmt.Errorf("seekTo requires offsetMs >= 0")
	}

	playerURL, err := url.Parse(player.URI)
	if err != nil {
		return fmt.Errorf("parse player URI: %w", err)
	}

	q := url.Values{}
	q.Set("X-Plex-Token", p.token)
	q.Set("type", "video")
	q.Set("commandID", "0")
	if action == "seekTo" {
		q.Set("offset", strconv.FormatInt(seekOffsetMs, 10))
	}

	playerURL.Path = "/player/playback/" + action
	playerURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playerURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build control request: %w", err)
	}
	req.Header.Set("X-Plex-Client-Identifier", "plex-dashboard")
	req.Header.Set("X-Plex-Target-Client-Identifier", player.ClientIdentifier)
	req.Header.Set("X-Plex-Product", "plex-dashboard")
	req.Header.Set("X-Plex-Version", "0.1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send playback control: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s failed for %q: status=%d body=%s", action, player.Name, resp.StatusCode, string(body))
	}
	return nil
}

func selectPlayer(players []Player, targetClientName string) (Player, error) {
	target := strings.TrimSpace(strings.ToLower(targetClientName))
	if target == "" {
		return Player{}, fmt.Errorf("target client name is required")
	}
	for _, player := range players {
		if strings.Contains(strings.ToLower(player.Name), target) {
			return player, nil
		}
	}
	// No name match — prefer the SSAP player (identified by ssap:// URI) if
	// one exists. This handles the common case where the configured name
	// ("Living Room") is stale but the LG TV is still discoverable via SSAP.
	for _, player := range players {
		if strings.HasPrefix(player.URI, "ssap://") {
			fmt.Printf("[player] %q not matched; falling back to SSAP player %q\n",
				targetClientName, player.Name)
			return player, nil
		}
	}
	// Last resort: only one player available.
	if len(players) == 1 {
		fmt.Printf("[player] %q not matched; falling back to only available player %q\n",
			targetClientName, players[0].Name)
		return players[0], nil
	}
	return Player{}, fmt.Errorf("no Plex player found matching %q (discovered: %d players)", targetClientName, len(players))
}

func filterMoviesByPeople(movies []Movie, actor, director string) []Movie {
	actor = strings.TrimSpace(strings.ToLower(actor))
	director = strings.TrimSpace(strings.ToLower(director))

	filtered := make([]Movie, 0)
	for _, movie := range movies {
		if actor != "" && !containsAny(movie.Actors, actor) {
			continue
		}
		if director != "" && !containsAny(movie.Directors, director) {
			continue
		}
		filtered = append(filtered, movie)
	}
	return filtered
}

func filterMoviesByGenreRatingYear(movies []Movie, genre string, minRating float64, minYear, maxYear int) []Movie {
	genre = strings.TrimSpace(strings.ToLower(genre))
	filtered := make([]Movie, 0)

	for _, movie := range movies {
		if movie.Year < minYear || movie.Year > maxYear {
			continue
		}
		if movie.Rating < minRating {
			continue
		}
		if genre != "" && !containsAny(movie.Genres, genre) {
			continue
		}
		filtered = append(filtered, movie)
	}

	return filtered
}

func containsAny(values []string, query string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

// ratingKeysOrderedByViewCountShuffleTies builds playlist order: lower Plex viewCount first
// (items watched less often play sooner), with a random shuffle within each view-count bucket.
func ratingKeysOrderedByViewCountShuffleTies(movies []Movie, rng *rand.Rand) []string {
	if len(movies) == 0 {
		return nil
	}
	buckets := make(map[int][]Movie)
	for _, m := range movies {
		c := m.ViewCount
		buckets[c] = append(buckets[c], m)
	}
	counts := make([]int, 0, len(buckets))
	for c := range buckets {
		counts = append(counts, c)
	}
	sort.Ints(counts)
	keys := make([]string, 0, len(movies))
	for _, c := range counts {
		bucket := buckets[c]
		rng.Shuffle(len(bucket), func(i, j int) { bucket[i], bucket[j] = bucket[j], bucket[i] })
		for _, m := range bucket {
			keys = append(keys, m.RatingKey)
		}
	}
	return keys
}

// ReorderMoviesByViewCountShuffleTies matches playlist ordering for previews (same rules as ratingKeysOrderedByViewCountShuffleTies).
func ReorderMoviesByViewCountShuffleTies(movies []Movie, rng *rand.Rand) []Movie {
	keys := ratingKeysOrderedByViewCountShuffleTies(movies, rng)
	byKey := make(map[string]Movie, len(movies))
	for _, m := range movies {
		byKey[m.RatingKey] = m
	}
	out := make([]Movie, 0, len(keys))
	for _, k := range keys {
		if m, ok := byKey[k]; ok {
			out = append(out, m)
		}
	}
	return out
}

func (p *PlexClient) listResources(ctx context.Context) (resourcesContainer, error) {
	body, err := p.get(ctx, "https://plex.tv/api/resources?includeHttps=1")
	if err != nil {
		return resourcesContainer{}, err
	}
	var resources resourcesContainer
	if err := xml.Unmarshal(body, &resources); err != nil {
		return resourcesContainer{}, fmt.Errorf("decode plex resources: %w", err)
	}
	return resources, nil
}

func (p *PlexClient) get(ctx context.Context, endpoint string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("X-Plex-Token", p.token)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex GET request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("plex GET %s failed: status=%d body=%s", u.Path, resp.StatusCode, string(body))
	}

	return body, nil
}

func (p *PlexClient) post(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("X-Plex-Token", p.token)
	for key, values := range form {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex POST request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("plex POST %s failed: status=%d body=%s", u.Path, resp.StatusCode, string(body))
	}

	return body, nil
}

type mediaContainer struct {
	XMLName xml.Name `xml:"MediaContainer"`
	Videos  []video  `xml:"Video"`
}

type mediaContainerRoot struct {
	XMLName           xml.Name `xml:"MediaContainer"`
	MachineIdentifier string   `xml:"machineIdentifier,attr"`
}

// parseTMDBIDFromPlexGuid extracts a TMDB movie id from Plex Video guid when the agent exposes it.
func parseTMDBIDFromPlexGuid(guid string) int {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return 0
	}
	// Examples: tmdb://12345, plex://movie/.../tmdb://12345, com.plexapp.agents.themoviedb://12345?lang=en
	if strings.HasPrefix(guid, "tmdb://") {
		return atoiPrefix(guid[len("tmdb://"):])
	}
	if i := strings.Index(guid, "tmdb://"); i >= 0 {
		return atoiPrefix(guid[i+len("tmdb://"):])
	}
	if i := strings.Index(guid, "themoviedb://"); i >= 0 {
		return atoiPrefix(guid[i+len("themoviedb://"):])
	}
	return 0
}

// parseIMDbIDFromPlexGuid extracts a normalized IMDb id (tt…) from Plex Video guid
// when the library uses the IMDb (or hybrid) agent, e.g. imdb://tt1234567.
func parseIMDbIDFromPlexGuid(guid string) string {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return ""
	}
	lower := strings.ToLower(guid)
	prefix := "imdb://"
	var tail string
	if strings.HasPrefix(lower, prefix) {
		tail = guid[len(prefix):]
	} else if i := strings.Index(lower, prefix); i >= 0 {
		tail = guid[i+len(prefix):]
	} else {
		return ""
	}
	return normalizeIMDbIDString(tail)
}

// normalizeIMDbIDString returns lowercase tt + digits, or "" if invalid.
func normalizeIMDbIDString(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.SplitN(s, "?", 2)[0]
	s = strings.TrimSuffix(s, "/")
	if !strings.HasPrefix(s, "tt") {
		return ""
	}
	j := 2
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j <= 2 {
		return ""
	}
	return s[:j]
}

func atoiPrefix(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.SplitN(s, "?", 2)[0]
	s = strings.SplitN(s, "/", 2)[0]
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

type plexVideoGuid struct {
	ID string `xml:"id,attr"`
}

type video struct {
	RatingKey    string          `xml:"ratingKey,attr"`
	Guid         string          `xml:"guid,attr"`
	GuidElems    []plexVideoGuid `xml:"Guid"`
	Title        string          `xml:"title,attr"`
	Year         string       `xml:"year,attr"`
	Duration     string       `xml:"duration,attr"`
	LastViewedAt string       `xml:"lastViewedAt,attr"`
	ViewCount    string       `xml:"viewCount,attr"`
	Rating       string       `xml:"rating,attr"`
	Summary      string       `xml:"summary,attr"`
	Studio       string       `xml:"studio,attr"`
	Roles        []mediaTag   `xml:"Role"`
	Directors    []mediaTag   `xml:"Director"`
	Genres       []mediaTag   `xml:"Genre"`
	Medias       []videoMedia `xml:"Media"`
}

type videoMedia struct {
	Container string      `xml:"container,attr"`
	Parts     []videoPart `xml:"Part"`
}

type videoPart struct {
	Key  string `xml:"key,attr"`
	Size string `xml:"size,attr"`
}

type mediaTag struct {
	Tag string `xml:"tag,attr"`
}

func tagsToStrings(tags []mediaTag) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.TrimSpace(tag.Tag) != "" {
			names = append(names, tag.Tag)
		}
	}
	return names
}

type resourcesContainer struct {
	XMLName xml.Name         `xml:"MediaContainer"`
	Devices []resourceDevice `xml:"Device"`
}

type playlistsContainer struct {
	XMLName   xml.Name       `xml:"MediaContainer"`
	Playlists []playlistItem `xml:"Playlist"`
}

type playlistItem struct {
	RatingKey string `xml:"ratingKey,attr"`
	Title     string `xml:"title,attr"`
}

type resourceDevice struct {
	Name             string               `xml:"name,attr"`
	Product          string               `xml:"product,attr"`
	Provides         string               `xml:"provides,attr"`
	ClientIdentifier string               `xml:"clientIdentifier,attr"`
	Connections      []resourceConnection `xml:"Connection"`
}

type resourceConnection struct {
	URI string `xml:"uri,attr"`
}
