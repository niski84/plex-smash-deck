package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const fanartMovieAPI = "https://webservice.fanart.tv/v3/movies"

type fanartImageRow struct {
	ID     string      `json:"id"`
	URL    string      `json:"url"`
	Lang   string      `json:"lang"`
	Likes  json.Number `json:"likes"`
	Width  json.Number `json:"width"`
	Height json.Number `json:"height"`
}

func (r fanartImageRow) likesInt() int {
	n, _ := strconv.Atoi(strings.TrimSpace(r.Likes.String()))
	return n
}

func (r fanartImageRow) area() int {
	w, _ := strconv.Atoi(strings.TrimSpace(r.Width.String()))
	h, _ := strconv.Atoi(strings.TrimSpace(r.Height.String()))
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func fanartRowDimensions(r fanartImageRow) (w, h int) {
	w, _ = strconv.Atoi(strings.TrimSpace(r.Width.String()))
	h, _ = strconv.Atoi(strings.TrimSpace(r.Height.String()))
	return w, h
}

func sortFanartImageRows(rows []fanartImageRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ai := rows[i].area()
		aj := rows[j].area()
		if ai != aj {
			return ai > aj
		}
		li, lj := rows[i].likesInt(), rows[j].likesInt()
		if li != lj {
			return li > lj
		}
		pref := func(lang string) int {
			switch strings.ToLower(strings.TrimSpace(lang)) {
			case "en", "00":
				return 2
			case "":
				return 1
			default:
				return 0
			}
		}
		return pref(rows[i].Lang) > pref(rows[j].Lang)
	})
}

func pickFanartBannerURL(payload map[string]json.RawMessage) (string, bool) {
	// Prefer wide backgrounds for a horizontal hero strip.
	priority := []string{
		"moviebackground",
		"hdmovieclearart",
		"movieposter",
		"hdmovielogo",
		"hdmovieclearlogo",
	}
	for _, key := range priority {
		raw, ok := payload[key]
		if !ok || len(raw) == 0 {
			continue
		}
		var rows []fanartImageRow
		if err := json.Unmarshal(raw, &rows); err != nil || len(rows) == 0 {
			continue
		}
		sortFanartImageRows(rows)
		u := strings.TrimSpace(rows[0].URL)
		if u != "" {
			return u, true
		}
	}
	return "", false
}

// FetchFanartMoviePayload loads fanart.tv JSON for a TMDB movie id (shared by banner + gallery).
func FetchFanartMoviePayload(ctx context.Context, tmdbID int, apiKey, clientKey string) (map[string]json.RawMessage, error) {
	if tmdbID <= 0 {
		return nil, fmt.Errorf("missing tmdb id")
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, fmt.Errorf("fanart api key not configured")
	}
	u, err := url.Parse(fanartMovieAPI)
	if err != nil {
		return nil, err
	}
	u.Path = fmt.Sprintf("/v3/movies/%d", tmdbID)
	q := u.Query()
	q.Set("api_key", key)
	if ck := strings.TrimSpace(clientKey); ck != "" {
		q.Set("client_key", ck)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("fanart: no art for tmdb %d", tmdbID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fanart: http %d", resp.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("fanart json: %w", err)
	}
	return payload, nil
}

// FetchFanartMovieBannerURL calls fanart.tv and returns the best banner-sized image URL.
func FetchFanartMovieBannerURL(ctx context.Context, tmdbID int, apiKey, clientKey string) (string, error) {
	payload, err := FetchFanartMoviePayload(ctx, tmdbID, apiKey, clientKey)
	if err != nil {
		return "", err
	}
	if picked, ok := pickFanartBannerURL(payload); ok {
		return picked, nil
	}
	return "", fmt.Errorf("fanart: no usable images for tmdb %d", tmdbID)
}

// FanartGalleryEntry is one fanart.tv image row for dashboard lightbox prefetch.
type FanartGalleryEntry struct {
	Kind   string `json:"kind"`
	URL    string `json:"-"` // remote URL; not serialized to client
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Label  string `json:"label"`
}

func fanartKindDisplayName(kind string) string {
	m := map[string]string{
		"moviebackground":  "Fanart · background",
		"hdmovieclearart":  "Fanart · clear art",
		"moviebanner":      "Fanart · banner",
		"moviethumb":       "Fanart · thumb",
		"movieposter":      "Fanart · poster",
		"hdmovielogo":      "Fanart · logo",
		"hdmovieclearlogo": "Fanart · clear logo",
	}
	if s, ok := m[kind]; ok {
		return s
	}
	return "Fanart · " + kind
}

// EnumerateFanartGallery lists images in display order (backgrounds first, then other kinds), up to maxImages.
func EnumerateFanartGallery(payload map[string]json.RawMessage, maxImages int) []FanartGalleryEntry {
	if maxImages <= 0 {
		maxImages = 48
	}
	order := []string{
		"moviebackground",
		"hdmovieclearart",
		"moviebanner",
		"moviethumb",
		"movieposter",
		"hdmovielogo",
		"hdmovieclearlogo",
	}
	var out []FanartGalleryEntry
	for _, key := range order {
		raw, ok := payload[key]
		if !ok || len(raw) == 0 {
			continue
		}
		var rows []fanartImageRow
		if err := json.Unmarshal(raw, &rows); err != nil || len(rows) == 0 {
			continue
		}
		sortFanartImageRows(rows)
		for _, row := range rows {
			u := strings.TrimSpace(row.URL)
			if u == "" {
				continue
			}
			w, h := fanartRowDimensions(row)
			out = append(out, FanartGalleryEntry{
				Kind: key, URL: u, Width: w, Height: h, Label: fanartKindDisplayName(key),
			})
			if len(out) >= maxImages {
				return out
			}
		}
	}
	return out
}
