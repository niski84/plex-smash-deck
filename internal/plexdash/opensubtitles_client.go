package plexdash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OpenSubtitlesClient is a tiny REST v1 wrapper.
//
// Endpoints used (https://api.opensubtitles.com/api/v1):
//   - POST /login     → bearer token (cached 24h)
//   - GET  /subtitles → search by imdb_id (no tt prefix), pick the first hit
//   - POST /download  → exchange file_id for a temporary URL
//   - GET  <url>      → SRT bytes
//
// All HTTP calls inherit the caller's ctx but each individual request enforces
// a 15s timeout via context.WithTimeout, so a slow remote can't stall callers.
type OpenSubtitlesClient struct {
	APIKey    string
	Username  string
	Password  string
	UserAgent string
	BaseURL   string
	HTTP      *http.Client

	mu             sync.Mutex
	token          string
	tokenExpiresAt time.Time
}

// NewOpenSubtitlesClient builds a client. If any of APIKey/Username/Password is
// blank, Available() returns false and Fetch() will not contact the network.
func NewOpenSubtitlesClient(apiKey, username, password, userAgent string) *OpenSubtitlesClient {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		ua = "plex-dashboard/0.1"
	}
	return &OpenSubtitlesClient{
		APIKey:    strings.TrimSpace(apiKey),
		Username:  strings.TrimSpace(username),
		Password:  strings.TrimSpace(password),
		UserAgent: ua,
		BaseURL:   "https://api.opensubtitles.com/api/v1",
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Available reports whether all required credentials are present.
func (c *OpenSubtitlesClient) Available() bool {
	if c == nil {
		return false
	}
	return c.APIKey != "" && c.Username != "" && c.Password != ""
}

// Fetch returns SRT bytes for an IMDB id (with or without "tt" prefix).
func (c *OpenSubtitlesClient) Fetch(ctx context.Context, imdbID string) ([]byte, error) {
	if !c.Available() {
		return nil, errors.New("opensubtitles: not configured")
	}
	imdbNum := strings.TrimPrefix(strings.TrimSpace(imdbID), "tt")
	if imdbNum == "" {
		return nil, errors.New("opensubtitles: empty imdbID")
	}

	fileID, err := c.searchFileID(ctx, imdbNum)
	if err != nil {
		return nil, err
	}
	if fileID == 0 {
		return nil, errors.New("opensubtitles: no subtitles for imdb_id=" + imdbNum)
	}

	link, err := c.requestDownloadLink(ctx, fileID)
	if err != nil {
		return nil, err
	}

	return c.downloadBytes(ctx, link)
}

// ── auth ─────────────────────────────────────────────────────────────────────

func (c *OpenSubtitlesClient) ensureToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	if !force && c.token != "" && time.Now().Before(c.tokenExpiresAt) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	body, _ := json.Marshal(map[string]string{
		"username": c.Username,
		"password": c.Password,
	})
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, c.BaseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.applyHeaders(req, "")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("opensubtitles login: status %d: %s", resp.StatusCode, string(b))
	}
	var lr struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", fmt.Errorf("opensubtitles login decode: %w", err)
	}
	if strings.TrimSpace(lr.Token) == "" {
		return "", errors.New("opensubtitles login: empty token")
	}

	c.mu.Lock()
	c.token = lr.Token
	c.tokenExpiresAt = time.Now().Add(24 * time.Hour)
	c.mu.Unlock()
	return lr.Token, nil
}

func (c *OpenSubtitlesClient) applyHeaders(req *http.Request, token string) {
	req.Header.Set("Api-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// ── search ───────────────────────────────────────────────────────────────────

func (c *OpenSubtitlesClient) searchFileID(ctx context.Context, imdbNum string) (int, error) {
	tok, err := c.ensureToken(ctx, false)
	if err != nil {
		return 0, err
	}
	id, status, err := c.doSearch(ctx, tok, imdbNum)
	if err != nil && status == http.StatusUnauthorized {
		log.Printf("[captions] opensubtitles 401 on /subtitles, re-logging in")
		tok2, lerr := c.ensureToken(ctx, true)
		if lerr != nil {
			return 0, lerr
		}
		id, _, err = c.doSearch(ctx, tok2, imdbNum)
	}
	return id, err
}

func (c *OpenSubtitlesClient) doSearch(ctx context.Context, token, imdbNum string) (int, int, error) {
	url := fmt.Sprintf("%s/subtitles?imdb_id=%s&languages=en", c.BaseURL, imdbNum)
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	c.applyHeaders(req, token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, resp.StatusCode, fmt.Errorf("opensubtitles search: status %d: %s", resp.StatusCode, string(b))
	}
	var sr struct {
		Data []struct {
			Attributes struct {
				Files []struct {
					FileID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return 0, resp.StatusCode, fmt.Errorf("opensubtitles search decode: %w", err)
	}
	for _, hit := range sr.Data {
		for _, f := range hit.Attributes.Files {
			if f.FileID > 0 {
				return f.FileID, resp.StatusCode, nil
			}
		}
	}
	return 0, resp.StatusCode, nil
}

// ── download ─────────────────────────────────────────────────────────────────

func (c *OpenSubtitlesClient) requestDownloadLink(ctx context.Context, fileID int) (string, error) {
	tok, err := c.ensureToken(ctx, false)
	if err != nil {
		return "", err
	}
	link, status, err := c.doDownloadLink(ctx, tok, fileID)
	if err != nil && status == http.StatusUnauthorized {
		log.Printf("[captions] opensubtitles 401 on /download, re-logging in")
		tok2, lerr := c.ensureToken(ctx, true)
		if lerr != nil {
			return "", lerr
		}
		link, _, err = c.doDownloadLink(ctx, tok2, fileID)
	}
	return link, err
}

func (c *OpenSubtitlesClient) doDownloadLink(ctx context.Context, token string, fileID int) (string, int, error) {
	body, _ := json.Marshal(map[string]any{"file_id": fileID})
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, c.BaseURL+"/download", bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	c.applyHeaders(req, token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", resp.StatusCode, fmt.Errorf("opensubtitles download: status %d: %s", resp.StatusCode, string(b))
	}
	var dr struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return "", resp.StatusCode, fmt.Errorf("opensubtitles download decode: %w", err)
	}
	if strings.TrimSpace(dr.Link) == "" {
		return "", resp.StatusCode, errors.New("opensubtitles download: empty link")
	}
	return dr.Link, resp.StatusCode, nil
}

func (c *OpenSubtitlesClient) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensubtitles fetch: status %d", resp.StatusCode)
	}
	// Cap at 4 MB — plain SRT files are typically well under 200 KB.
	return io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
}
