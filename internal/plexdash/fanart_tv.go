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
		u := strings.TrimSpace(rows[0].URL)
		if u != "" {
			return u, true
		}
	}
	return "", false
}

// FetchFanartMovieBannerURL calls fanart.tv and returns the best banner-sized image URL.
func FetchFanartMovieBannerURL(ctx context.Context, tmdbID int, apiKey, clientKey string) (string, error) {
	if tmdbID <= 0 {
		return "", fmt.Errorf("missing tmdb id")
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return "", fmt.Errorf("fanart api key not configured")
	}
	u, err := url.Parse(fanartMovieAPI)
	if err != nil {
		return "", err
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
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("fanart: no art for tmdb %d", tmdbID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fanart: http %d", resp.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("fanart json: %w", err)
	}
	if picked, ok := pickFanartBannerURL(payload); ok {
		return picked, nil
	}
	return "", fmt.Errorf("fanart: no usable images for tmdb %d", tmdbID)
}
