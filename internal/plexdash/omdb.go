package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func defaultOMDbCacheDir() string {
	return filepath.Clean("data/omdb-cache")
}

// parseOMDbIMDbRating parses OMDb "imdbRating" (e.g. "8.1/10") to 0–10.
func parseOMDbIMDbRating(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0, false
	}
	base := s
	if i := strings.Index(s, "/"); i > 0 {
		base = strings.TrimSpace(s[:i])
	}
	v, err := strconv.ParseFloat(base, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

type cachedOMDbRating struct {
	CachedAt time.Time `json:"cachedAt"`
	Rating   float64   `json:"rating"`
	OK       bool      `json:"ok"`
}

var omdbCacheMu sync.Mutex

func omdbCachePath(imdbID string) string {
	id := strings.TrimSpace(strings.ToLower(imdbID))
	id = strings.ReplaceAll(id, string(filepath.Separator), "")
	return filepath.Join(defaultOMDbCacheDir(), "rating-"+id+".json")
}

func readOMDbRatingCache(imdbID string) (float64, bool, bool) {
	omdbCacheMu.Lock()
	defer omdbCacheMu.Unlock()
	p := omdbCachePath(imdbID)
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false, false
	}
	var c cachedOMDbRating
	if json.Unmarshal(b, &c) != nil {
		return 0, false, false
	}
	if !c.OK {
		return 0, false, true
	}
	return c.Rating, true, true
}

func writeOMDbRatingCache(imdbID string, rating float64, ok bool) {
	omdbCacheMu.Lock()
	defer omdbCacheMu.Unlock()
	dir := defaultOMDbCacheDir()
	_ = os.MkdirAll(dir, 0o755)
	p := omdbCachePath(imdbID)
	c := cachedOMDbRating{CachedAt: time.Now().UTC(), Rating: rating, OK: ok}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// omdbFetchVoteAverage returns IMDb-weighted community score from OMDb (0–10), when available.
func omdbFetchVoteAverage(ctx context.Context, apiKey, imdbID string) (float64, bool, error) {
	apiKey = strings.TrimSpace(apiKey)
	imdbID = strings.TrimSpace(imdbID)
	if apiKey == "" || imdbID == "" {
		return 0, false, nil
	}
	if v, has, hit := readOMDbRatingCache(imdbID); hit {
		if !has {
			return 0, false, nil
		}
		return v, true, nil
	}

	u := "https://www.omdbapi.com/?i=" + url.QueryEscape(imdbID) + "&apikey=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeOMDbRatingCache(imdbID, 0, false)
		return 0, false, fmt.Errorf("omdb: status %s", resp.Status)
	}
	var out struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, false, err
	}
	if strings.EqualFold(out.Response, "False") {
		writeOMDbRatingCache(imdbID, 0, false)
		return 0, false, nil
	}
	r, ok := parseOMDbIMDbRating(out.IMDbRating)
	if !ok {
		writeOMDbRatingCache(imdbID, 0, false)
		return 0, false, nil
	}
	writeOMDbRatingCache(imdbID, r, true)
	return r, true, nil
}

// blendVoteWithOMDb averages TMDB vote average (0–10) with OMDb imdbRating when both exist and blending is enabled.
func blendVoteWithOMDb(ctx context.Context, cfg Config, tmdbVote float64, imdbID string) float64 {
	if !cfg.OMDbBlendRatings || strings.TrimSpace(cfg.OMDbAPIKey) == "" {
		return tmdbVote
	}
	imdbID = strings.TrimSpace(imdbID)
	if imdbID == "" || !strings.HasPrefix(strings.ToLower(imdbID), "tt") {
		return tmdbVote
	}
	omdb, ok, err := omdbFetchVoteAverage(ctx, cfg.OMDbAPIKey, imdbID)
	if err != nil || !ok || omdb <= 0 {
		return tmdbVote
	}
	return (tmdbVote + omdb) / 2
}

// applyOMDbBlendToDiscoveryItems fetches IMDb ids via TMDB and re-averages ratings (mutates slice, re-sorts).
func applyOMDbBlendToDiscoveryItems(ctx context.Context, cfg Config, items []DiscoveryItem, tmdbCache *diskDiscoveryCache) []DiscoveryItem {
	if !cfg.OMDbBlendRatings || strings.TrimSpace(cfg.OMDbAPIKey) == "" || len(items) == 0 {
		return items
	}
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range items {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			imdb, err := tmdbMovieExternalIDsCached(ctx, cfg.TMDBAPIKey, items[i].TMDBID, tmdbCache)
			if err != nil || strings.TrimSpace(imdb) == "" {
				return
			}
			blended := blendVoteWithOMDb(ctx, cfg, items[i].VoteAverage, imdb)
			mu.Lock()
			items[i].VoteAverage = blended
			mu.Unlock()
		}()
	}
	wg.Wait()
	sortDiscoveryItems(items)
	return items
}

// RemoveAllOMDbCache deletes on-disk OMDb rating cache files.
func RemoveAllOMDbCache() (int, error) {
	dir := defaultOMDbCacheDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
