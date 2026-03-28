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

// parseRottenTomatoesPercent maps OMDb RT values like "85%" to a 0–10 score (8.5).
func parseRottenTomatoesPercent(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return 0, false
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v / 10.0, true
}

// parseMetacriticSlash maps "75/100" to 7.5 (0–10).
func parseMetacriticSlash(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return 0, false
	}
	if i := strings.Index(s, "/"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v / 10.0, true
}

// parseMetascoreField maps standalone Metascore "75" to 7.5 (0–10).
func parseMetascoreField(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v / 10.0, true
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

func omdbFullCachePath(imdbID string) string {
	id := strings.TrimSpace(strings.ToLower(imdbID))
	id = strings.ReplaceAll(id, string(filepath.Separator), "")
	return filepath.Join(defaultOMDbCacheDir(), "full-"+id+".json")
}

// OMDbRatingEntry is one normalized score for dashboard / hover UI (0–10 scale).
type OMDbRatingEntry struct {
	Source  string  `json:"source"`
	Label   string  `json:"label"`
	Display string  `json:"display"`
	Score10 float64 `json:"score10"` // 0–10 for averaging (RT % and Metacritic /100 mapped)
}

type cachedOMDbFull struct {
	CachedAt time.Time         `json:"cachedAt"`
	OK       bool              `json:"ok"`
	IMDbID   string            `json:"imdbId"`
	Entries  []OMDbRatingEntry `json:"entries"`
}

func readOMDbFullCache(imdbID string) (cachedOMDbFull, bool) {
	omdbCacheMu.Lock()
	defer omdbCacheMu.Unlock()
	p := omdbFullCachePath(imdbID)
	b, err := os.ReadFile(p)
	if err != nil {
		return cachedOMDbFull{}, false
	}
	var c cachedOMDbFull
	if json.Unmarshal(b, &c) != nil {
		return cachedOMDbFull{}, false
	}
	return c, true
}

func writeOMDbFullCache(c cachedOMDbFull) {
	imdbID := strings.TrimSpace(c.IMDbID)
	if imdbID == "" {
		return
	}
	omdbCacheMu.Lock()
	defer omdbCacheMu.Unlock()
	dir := defaultOMDbCacheDir()
	_ = os.MkdirAll(dir, 0o755)
	p := omdbFullCachePath(imdbID)
	c.CachedAt = time.Now().UTC()
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

func averageScore10(entries []OMDbRatingEntry) float64 {
	if len(entries) == 0 {
		return 0
	}
	var sum float64
	n := 0
	for _, e := range entries {
		if e.Score10 > 0 {
			sum += e.Score10
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func parseOMDbJSONToDetail(imdbID string, body []byte) cachedOMDbFull {
	out := cachedOMDbFull{IMDbID: strings.TrimSpace(imdbID), OK: false, Entries: nil}
	var raw struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
		Metascore  string `json:"Metascore"`
		Ratings    []struct {
			Source string `json:"Source"`
			Value  string `json:"Value"`
		} `json:"Ratings"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return out
	}
	if strings.EqualFold(raw.Response, "False") {
		return out
	}
	var entries []OMDbRatingEntry
	haveIMDb := false
	for _, r := range raw.Ratings {
		val := strings.TrimSpace(r.Value)
		ls := strings.ToLower(strings.TrimSpace(r.Source))
		switch {
		case strings.Contains(ls, "internet movie"):
			if v, ok := parseOMDbIMDbRating(val); ok {
				entries = append(entries, OMDbRatingEntry{Source: "imdb", Label: "IMDb", Display: val, Score10: v})
				haveIMDb = true
			}
		case strings.Contains(ls, "rotten"):
			if v, ok := parseRottenTomatoesPercent(val); ok {
				entries = append(entries, OMDbRatingEntry{Source: "rottenTomatoes", Label: "RT", Display: val, Score10: v})
			}
		case strings.Contains(ls, "metacritic"):
			if v, ok := parseMetacriticSlash(val); ok {
				entries = append(entries, OMDbRatingEntry{Source: "metacritic", Label: "Metacritic", Display: val, Score10: v})
			}
		}
	}
	if !haveIMDb {
		if v, ok := parseOMDbIMDbRating(raw.IMDbRating); ok {
			disp := strings.TrimSpace(raw.IMDbRating)
			if disp == "" {
				disp = fmt.Sprintf("%.1f/10", v)
			}
			entries = append(entries, OMDbRatingEntry{Source: "imdb", Label: "IMDb", Display: disp, Score10: v})
			haveIMDb = true
		}
	}
	if v, ok := parseMetascoreField(raw.Metascore); ok {
		hasMC := false
		for _, e := range entries {
			if e.Source == "metacritic" {
				hasMC = true
				break
			}
		}
		if !hasMC {
			ms := strings.TrimSpace(raw.Metascore)
			entries = append(entries, OMDbRatingEntry{Source: "metacritic", Label: "Metacritic", Display: ms + "/100", Score10: v})
		}
	}
	out.Entries = entries
	out.OK = len(entries) > 0
	return out
}

// FetchOMDbRatingsDetail returns IMDb / RT / Metacritic-style rows from OMDb (cached under data/omdb-cache/full-*.json).
// On a successful parse it also refreshes the legacy rating-* cache used for Discovery blending.
func FetchOMDbRatingsDetail(ctx context.Context, apiKey, imdbID string) (cachedOMDbFull, error) {
	apiKey = strings.TrimSpace(apiKey)
	imdbID = strings.TrimSpace(imdbID)
	empty := cachedOMDbFull{IMDbID: imdbID}
	if apiKey == "" || imdbID == "" || !strings.HasPrefix(strings.ToLower(imdbID), "tt") {
		return empty, nil
	}
	if c, hit := readOMDbFullCache(imdbID); hit {
		if !c.OK {
			return c, nil
		}
		return c, nil
	}
	u := "https://www.omdbapi.com/?i=" + url.QueryEscape(imdbID) + "&apikey=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return empty, err
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c := cachedOMDbFull{IMDbID: imdbID, OK: false, Entries: nil}
		writeOMDbFullCache(c)
		writeOMDbRatingCache(imdbID, 0, false)
		return c, fmt.Errorf("omdb: status %s", resp.Status)
	}
	detail := parseOMDbJSONToDetail(imdbID, body)
	writeOMDbFullCache(detail)
	var imdbForBlend float64
	var imdbBlendOK bool
	for _, e := range detail.Entries {
		if e.Source == "imdb" && e.Score10 > 0 {
			imdbForBlend = e.Score10
			imdbBlendOK = true
			break
		}
	}
	if imdbBlendOK {
		writeOMDbRatingCache(imdbID, imdbForBlend, true)
	} else if strings.Contains(string(body), `"Response":"False"`) || strings.Contains(string(body), `"Response": "False"`) {
		writeOMDbRatingCache(imdbID, 0, false)
	} else if !detail.OK {
		writeOMDbRatingCache(imdbID, 0, false)
	}
	return detail, nil
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

// RemoveAllOMDbCache deletes on-disk OMDb cache files (rating-* and full-*).
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
		name := e.Name()
		if !strings.HasPrefix(name, "rating-") && !strings.HasPrefix(name, "full-") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
