package plexdash

import (
	"context"
	"encoding/xml"
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
	baseURL string
	token   string
	client  *http.Client
}

type Movie struct {
	RatingKey         string
	Title             string
	Year              int
	DurationMillis    int64
	LastViewedAtEpoch int64
	Rating            float64
	Actors            []string
	Directors         []string
	Genres            []string
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
}

type Player struct {
	Name             string
	ClientIdentifier string
	Product          string
	URI              string
}

type Playlist struct {
	RatingKey string
	Title     string
}

func NewPlexClient(cfg Config) *PlexClient {
	return &PlexClient{
		baseURL: cfg.PlexBaseURL,
		token:   cfg.PlexToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *PlexClient) IsConfigured() bool {
	return strings.TrimSpace(p.baseURL) != "" && strings.TrimSpace(p.token) != ""
}

func (p *PlexClient) ListMovies(ctx context.Context, libraryKey string) ([]Movie, error) {
	endpoint := fmt.Sprintf("%s/library/sections/%s/all", p.baseURL, libraryKey)
	body, err := p.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var root mediaContainer
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode movie list: %w", err)
	}

	movies := make([]Movie, 0, len(root.Videos))
	for _, video := range root.Videos {
		year, _ := strconv.Atoi(video.Year)
		duration, _ := strconv.ParseInt(video.Duration, 10, 64)
		lastViewedAt, _ := strconv.ParseInt(video.LastViewedAt, 10, 64)
		rating, _ := strconv.ParseFloat(video.Rating, 64)

		movies = append(movies, Movie{
			RatingKey:         video.RatingKey,
			Title:             video.Title,
			Year:              year,
			DurationMillis:    duration,
			LastViewedAtEpoch: lastViewedAt,
			Rating:            rating,
			Actors:            tagsToStrings(video.Roles),
			Directors:         tagsToStrings(video.Directors),
			Genres:            tagsToStrings(video.Genres),
		})
	}

	sort.SliceStable(movies, func(i, j int) bool {
		return strings.ToLower(movies[i].Title) < strings.ToLower(movies[j].Title)
	})

	return movies, nil
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

	indices := rand.New(rand.NewSource(time.Now().UnixNano())).Perm(len(movies))[:count]
	ratingKeys := make([]string, 0, count)
	for _, idx := range indices {
		ratingKeys = append(ratingKeys, movies[idx].RatingKey)
	}
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

	indices := rand.New(rand.NewSource(time.Now().UnixNano())).Perm(len(filtered))[:count]
	ratingKeys := make([]string, 0, count)
	for _, idx := range indices {
		ratingKeys = append(ratingKeys, filtered[idx].RatingKey)
	}

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

	ratingKeys := make([]string, 0, len(filtered))
	for _, movie := range filtered {
		ratingKeys = append(ratingKeys, movie.RatingKey)
	}

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

func (p *PlexClient) ListPlayers(ctx context.Context) ([]Player, error) {
	resources, err := p.listResources(ctx)
	if err != nil {
		return nil, err
	}

	players := make([]Player, 0)
	for _, device := range resources.Devices {
		if !strings.Contains(device.Provides, "player") {
			continue
		}
		if len(device.Connections) == 0 {
			continue
		}
		players = append(players, Player{
			Name:             device.Name,
			ClientIdentifier: device.ClientIdentifier,
			Product:          device.Product,
			URI:              device.Connections[0].URI,
		})
	}

	sort.SliceStable(players, func(i, j int) bool {
		return strings.ToLower(players[i].Name) < strings.ToLower(players[j].Name)
	})
	return players, nil
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

		movies = append(movies, Movie{
			RatingKey:         video.RatingKey,
			Title:             video.Title,
			Year:              year,
			DurationMillis:    duration,
			LastViewedAtEpoch: lastViewedAt,
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
	items, err := p.PlaylistMovies(ctx, playlistTitle, 1)
	if err != nil {
		return CreateAndPlayResult{}, err
	}
	if len(items) == 0 {
		return CreateAndPlayResult{}, fmt.Errorf("playlist %q has no items", playlistTitle)
	}

	players, err := p.ListPlayers(ctx)
	if err != nil {
		return CreateAndPlayResult{}, err
	}
	target, err := selectPlayer(players, targetClientName)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	if err := p.playMovieOnClient(ctx, target, items[0].RatingKey); err != nil {
		return CreateAndPlayResult{}, err
	}

	return CreateAndPlayResult{
		PlaylistTitle: playlistTitle,
		PlaylistCount: 0,
		TargetClient:  target.Name,
		PlaybackKey:   items[0].RatingKey,
	}, nil
}

func (p *PlexClient) CreateRandomPlaylistAndPlay(ctx context.Context, libraryKey, playlistTitle string, count int, targetClientName string) (CreateAndPlayResult, error) {
	created, err := p.CreateRandomPlaylist(ctx, libraryKey, playlistTitle, count)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	players, err := p.ListPlayers(ctx)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	target, err := selectPlayer(players, targetClientName)
	if err != nil {
		return CreateAndPlayResult{}, err
	}

	if err := p.playMovieOnClient(ctx, target, created.FirstRatingKey); err != nil {
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
	return Player{}, fmt.Errorf("no Plex player found matching %q", targetClientName)
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

type video struct {
	RatingKey    string     `xml:"ratingKey,attr"`
	Title        string     `xml:"title,attr"`
	Year         string     `xml:"year,attr"`
	Duration     string     `xml:"duration,attr"`
	LastViewedAt string     `xml:"lastViewedAt,attr"`
	Rating       string     `xml:"rating,attr"`
	Roles        []mediaTag `xml:"Role"`
	Directors    []mediaTag `xml:"Director"`
	Genres       []mediaTag `xml:"Genre"`
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
