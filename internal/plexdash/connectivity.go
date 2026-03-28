package plexdash

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// connectivityProbeInterval spaces out lightweight checks (identity, etc.).
	connectivityProbeInterval = 45 * time.Second
	connectivityProbeTimeout  = 8 * time.Second

	plexStreamProbeInterval = 4 * time.Minute
	streamProbeMaxBytes     = 512 * 1024
	streamProbeSlowMbps     = 12.0
	streamProbeHTTPTimeout  = 22 * time.Second
	streamProbeMinBytes     = 16 * 1024
)

// ConnectivityCheck is one row in the connectivity dashboard (JSON for the UI).
type ConnectivityCheck struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Level     string `json:"level"` // ok | warn | error | skip
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

// PlexStreamMetrics is a periodic sample read from one library file (not full playback).
type PlexStreamMetrics struct {
	Level      string  `json:"level"` // ok | warn | skip
	Message    string  `json:"message"`
	Mbps       float64 `json:"mbps,omitempty"`
	BytesRead  int64   `json:"bytesRead,omitempty"`
	MsTotal    int64   `json:"msTotal,omitempty"`
	ProbedAt   string  `json:"probedAt,omitempty"`
	MovieTitle string  `json:"movieTitle,omitempty"`
}

// ConnectivityPayload is returned by GET /api/connectivity.
type ConnectivityPayload struct {
	UpdatedAt  string              `json:"updatedAt"`
	Overall    string              `json:"overall"` // ok | warn | error
	Summary    string              `json:"summary"`
	Checks     []ConnectivityCheck `json:"checks"`
	PlexStream PlexStreamMetrics   `json:"plexStream"`
}

// Server fields (see server.go): connMu, connectivity atomic snapshot.

func (s *Server) runConnectivityProbe(ctx context.Context) {
	cfg := s.snapshot()
	tctx, cancel := context.WithTimeout(ctx, connectivityProbeTimeout)
	checks := []ConnectivityCheck{
		probeInternet(tctx),
		probePlex(tctx, cfg),
		probeTMDB(tctx, cfg),
		probeOMDb(tctx, cfg),
		probeLG(tctx, cfg),
	}
	cancel()

	s.mlMu.RLock()
	movies := s.mlMovies
	s.mlMu.RUnlock()

	streamCtx, cancelStream := context.WithTimeout(context.Background(), streamProbeHTTPTimeout)
	ps := s.maybeProbePlexStream(streamCtx, cfg, movies)
	cancelStream()

	overall, summary := summarizeConnectivity(checks, ps)
	out := ConnectivityPayload{
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		Overall:    overall,
		Summary:    summary,
		Checks:     checks,
		PlexStream: ps,
	}
	s.connMu.Lock()
	s.connectivity = out
	s.connMu.Unlock()
}

// StartConnectivityProbes runs an immediate probe then repeats on an interval until ctx is done.
func (s *Server) StartConnectivityProbes(ctx context.Context) {
	s.runConnectivityProbe(context.Background())
	t := time.NewTicker(connectivityProbeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runConnectivityProbe(context.Background())
		}
	}
}

func (s *Server) snapshotConnectivity() ConnectivityPayload {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	if s.connectivity.UpdatedAt == "" {
		return ConnectivityPayload{
			Overall: "warn",
			Summary: "Waiting for first connectivity probe…",
			Checks:  nil,
		}
	}
	return s.connectivity
}

func probeInternet(ctx context.Context) ConnectivityCheck {
	const id = "internet"
	label := "Internet"
	t0 := time.Now()
	d := &net.Dialer{Timeout: 4 * time.Second}

	conn, err := d.DialContext(ctx, "tcp", "1.1.1.1:443")
	if err == nil {
		_ = conn.Close()
		return ConnectivityCheck{
			ID: id, Label: label, Level: "ok",
			Message:   "Reachable via Cloudflare (1.1.1.1:443)",
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
	errCF := err

	conn2, err2 := d.DialContext(ctx, "tcp", "8.8.8.8:53")
	if err2 == nil {
		_ = conn2.Close()
		return ConnectivityCheck{
			ID: id, Label: label, Level: "ok",
			Message:   "Reachable via Google DNS (8.8.8.8:53)",
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}

	return ConnectivityCheck{
		ID: id, Label: label, Level: "error",
		Message: fmt.Sprintf(
			"Cannot reach the internet (Cloudflare 1.1.1.1:443 and Google 8.8.8.8:53 failed: %v; %v)",
			errCF, err2,
		),
		LatencyMs: time.Since(t0).Milliseconds(),
	}
}

func probePlex(ctx context.Context, cfg Config) ConnectivityCheck {
	const id = "plex"
	label := "Plex server"
	base := strings.TrimSpace(cfg.PlexBaseURL)
	token := strings.TrimSpace(cfg.PlexToken)
	if base == "" || token == "" {
		return ConnectivityCheck{ID: id, Label: label, Level: "skip", Message: "Plex URL or token not configured"}
	}
	if _, err := url.Parse(base); err != nil {
		return ConnectivityCheck{ID: id, Label: label, Level: "error", Message: "Invalid Plex base URL"}
	}
	identity := strings.TrimRight(base, "/") + "/identity"
	t0 := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, identity, nil)
	if err != nil {
		return ConnectivityCheck{ID: id, Label: label, Level: "error", Message: err.Error()}
	}
	q := req.URL.Query()
	q.Set("X-Plex-Token", token)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityCheck{
			ID: id, Label: label, Level: "error",
			Message:   fmt.Sprintf("Cannot reach Plex: %v", err),
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ConnectivityCheck{
			ID: id, Label: label, Level: "warn",
			Message:   "Plex returned 401 — check token",
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return ConnectivityCheck{
			ID: id, Label: label, Level: "error",
			Message:   fmt.Sprintf("Plex returned HTTP %d", resp.StatusCode),
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	default:
		return ConnectivityCheck{
			ID: id, Label: label, Level: "ok",
			Message:   "Plex Media Server reachable",
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
}

func probeTMDB(ctx context.Context, cfg Config) ConnectivityCheck {
	const id = "tmdb"
	label := "TMDB API"
	key := strings.TrimSpace(cfg.TMDBAPIKey)
	if key == "" {
		return ConnectivityCheck{ID: id, Label: label, Level: "skip", Message: "TMDB API key not configured"}
	}
	t0 := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}
	u := "https://api.themoviedb.org/3/configuration?api_key=" + url.QueryEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ConnectivityCheck{ID: id, Label: label, Level: "warn", Message: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityCheck{
			ID: id, Label: label, Level: "warn",
			Message:   fmt.Sprintf("Cannot reach TMDB: %v", err),
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ConnectivityCheck{
			ID: id, Label: label, Level: "warn",
			Message:   "TMDB rejected API key (401)",
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return ConnectivityCheck{
			ID: id, Label: label, Level: "warn",
			Message:   fmt.Sprintf("TMDB returned HTTP %d", resp.StatusCode),
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	default:
		return ConnectivityCheck{
			ID: id, Label: label, Level: "ok",
			Message:   "TMDB API reachable",
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
}

func probeOMDb(ctx context.Context, cfg Config) ConnectivityCheck {
	const id = "omdb"
	label := "OMDb API"
	key := strings.TrimSpace(cfg.OMDbAPIKey)
	if key == "" {
		return ConnectivityCheck{ID: id, Label: label, Level: "skip", Message: "OMDb API key not configured (optional)"}
	}
	t0 := time.Now()
	u := "https://www.omdbapi.com/?i=tt3896198&apikey=" + url.QueryEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ConnectivityCheck{ID: id, Label: label, Level: "error", Message: err.Error(), LatencyMs: time.Since(t0).Milliseconds()}
	}
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityCheck{ID: id, Label: label, Level: "error", Message: err.Error(), LatencyMs: time.Since(t0).Milliseconds()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ConnectivityCheck{
			ID: id, Label: label, Level: "warn",
			Message:   fmt.Sprintf("HTTP %d", resp.StatusCode),
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
	var out struct {
		Response string `json:"Response"`
		Error    string `json:"Error"`
	}
	_ = json.Unmarshal(body, &out)
	if strings.EqualFold(out.Response, "False") {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = "OMDb returned Response=False"
		}
		return ConnectivityCheck{ID: id, Label: label, Level: "warn", Message: msg, LatencyMs: time.Since(t0).Milliseconds()}
	}
	return ConnectivityCheck{
		ID: id, Label: label, Level: "ok",
		Message:   "OMDb API reachable",
		LatencyMs: time.Since(t0).Milliseconds(),
	}
}

func probeLG(ctx context.Context, cfg Config) ConnectivityCheck {
	const id = "lgtv"
	label := "LG TV (SSAP)"
	ip := strings.TrimSpace(cfg.LGTVAddr)
	if ip == "" {
		return ConnectivityCheck{ID: id, Label: label, Level: "skip", Message: "LG TV IP not configured (LGTV_ADDR)"}
	}
	addr := net.JoinHostPort(ip, "3001")
	t0 := time.Now()
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 4 * time.Second},
		Config:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec — TV uses self-signed cert
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return ConnectivityCheck{
			ID: id, Label: label, Level: "warn",
			Message:   fmt.Sprintf("Cannot reach TV SSAP port 3001: %v", err),
			LatencyMs: time.Since(t0).Milliseconds(),
		}
	}
	_ = conn.Close()
	return ConnectivityCheck{
		ID: id, Label: label, Level: "ok",
		Message:   "TV accepts TLS on port 3001 (SSAP)",
		LatencyMs: time.Since(t0).Milliseconds(),
	}
}

func summarizeConnectivity(checks []ConnectivityCheck, stream PlexStreamMetrics) (overall, summary string) {
	byID := make(map[string]ConnectivityCheck, len(checks))
	for _, c := range checks {
		byID[c.ID] = c
	}
	if inet, ok := byID["internet"]; ok && inet.Level == "error" {
		return "error", inet.Message
	}

	var firstErr string
	for _, c := range checks {
		if c.Level == "error" {
			firstErr = c.Message
			break
		}
	}
	if firstErr != "" {
		return "error", firstErr
	}

	var warns []string
	for _, c := range checks {
		if c.Level == "warn" {
			warns = append(warns, c.Label+": "+c.Message)
		}
	}
	if stream.Level == "warn" && strings.TrimSpace(stream.Message) != "" {
		warns = append(warns, stream.Message)
	}
	if len(warns) > 0 {
		return "warn", strings.Join(warns, " · ")
	}
	if stream.Level == "ok" && stream.Mbps > 0 && strings.TrimSpace(stream.ProbedAt) != "" {
		return "ok", fmt.Sprintf("All OK · ~%.1f Mb/s to Plex", stream.Mbps)
	}
	return "ok", "All connectivity checks passed"
}

func pickMovieForStreamProbe(movies []Movie) (Movie, bool) {
	var best *Movie
	for i := range movies {
		m := &movies[i]
		if strings.TrimSpace(m.PartKey) == "" {
			continue
		}
		if best == nil {
			best = m
			continue
		}
		// Prefer smallest on-disk file to minimize Plex disk work for the sample.
		switch {
		case m.PartSize > 0 && best.PartSize > 0 && m.PartSize < best.PartSize:
			best = m
		case best.PartSize <= 0 && m.PartSize > 0:
			best = m
		}
	}
	if best == nil {
		return Movie{}, false
	}
	return *best, true
}

func plexPartMediaURL(base, token, partKey string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	partKey = strings.TrimSpace(partKey)
	if !strings.HasPrefix(partKey, "/") {
		partKey = "/" + partKey
	}
	q := "X-Plex-Token=" + url.QueryEscape(strings.TrimSpace(token))
	if strings.Contains(partKey, "?") {
		return base + partKey + "&" + q
	}
	return base + partKey + "?" + q
}

func fetchPlexStreamSample(ctx context.Context, streamURL string) (int64, int, error) {
	client := &http.Client{Timeout: streamProbeHTTPTimeout}

	do := func(rangeHeader string) (n int64, code int, err error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
		if err != nil {
			return 0, 0, err
		}
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, 0, err
		}
		defer resp.Body.Close()
		code = resp.StatusCode
		if code != http.StatusOK && code != http.StatusPartialContent {
			_, _ = io.Copy(io.Discard, resp.Body)
			return 0, code, fmt.Errorf("HTTP %d", code)
		}
		n, err = io.Copy(io.Discard, io.LimitReader(resp.Body, streamProbeMaxBytes))
		return n, code, err
	}

	rangeHdr := "bytes=0-" + strconv.Itoa(streamProbeMaxBytes-1)
	n, code, err := do(rangeHdr)
	if code == http.StatusRequestedRangeNotSatisfiable {
		n, code, err = do("")
	}
	return n, code, err
}

func executePlexStreamProbe(ctx context.Context, cfg Config, m Movie) PlexStreamMetrics {
	now := time.Now().UTC().Format(time.RFC3339)
	streamURL := plexPartMediaURL(cfg.PlexBaseURL, cfg.PlexToken, m.PartKey)
	t0 := time.Now()
	n, _, err := fetchPlexStreamSample(ctx, streamURL)
	ms := time.Since(t0).Milliseconds()
	title := strings.TrimSpace(m.Title)
	if err != nil {
		return PlexStreamMetrics{
			Level:      "warn",
			Message:    fmt.Sprintf("Could not measure Plex speed: %v", err),
			BytesRead:  n,
			MsTotal:    ms,
			ProbedAt:   now,
			MovieTitle: title,
		}
	}
	if n < streamProbeMinBytes {
		return PlexStreamMetrics{
			Level:      "warn",
			Message:    fmt.Sprintf("Sample too small to score speed (%d B)", n),
			BytesRead:  n,
			MsTotal:    ms,
			ProbedAt:   now,
			MovieTitle: title,
		}
	}
	sec := float64(ms) / 1000.0
	if sec < 0.001 {
		sec = 0.001
	}
	mbps := (float64(n) * 8) / sec / 1e6
	msg := fmt.Sprintf("~%.1f Mb/s to Plex (quick test, %q)", mbps, title)
	level := "ok"
	if mbps < streamProbeSlowMbps {
		level = "warn"
		msg += " — on the slow side. Heavy movies may buffer; orange/red file sizes on posters mark the demanding ones (TV Wi-Fi can differ)."
	}
	return PlexStreamMetrics{
		Level:      level,
		Message:    msg,
		Mbps:       mbps,
		BytesRead:  n,
		MsTotal:    ms,
		ProbedAt:   now,
		MovieTitle: title,
	}
}

func formatByteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
}

// maybeProbePlexStream rate-limits Plex media reads (one short ranged GET every plexStreamProbeInterval).
func (s *Server) maybeProbePlexStream(ctx context.Context, cfg Config, movies []Movie) PlexStreamMetrics {
	base := strings.TrimSpace(cfg.PlexBaseURL)
	token := strings.TrimSpace(cfg.PlexToken)
	if base == "" || token == "" {
		return PlexStreamMetrics{Level: "skip", Message: "Plex not configured"}
	}

	movie, ok := pickMovieForStreamProbe(movies)
	if !ok {
		return PlexStreamMetrics{
			Level:   "skip",
			Message: "No movies in server memory — open Dashboard and Refresh Movies to enable stream sampling",
		}
	}

	s.streamProbeMu.Lock()
	defer s.streamProbeMu.Unlock()

	if !s.lastStreamProbeAt.IsZero() && time.Since(s.lastStreamProbeAt) < plexStreamProbeInterval {
		return s.plexStreamCache
	}

	s.lastStreamProbeAt = time.Now()
	s.plexStreamCache = executePlexStreamProbe(ctx, cfg, movie)
	return s.plexStreamCache
}

func (s *Server) handleConnectivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	payload := s.snapshotConnectivity()
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: payload})
}
