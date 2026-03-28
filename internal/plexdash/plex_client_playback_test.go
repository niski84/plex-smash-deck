package plexdash

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPlaybackSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status/sessions" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<MediaContainer size="1">
<Video type="movie" title="Test Movie" year="2020" viewOffset="300000" duration="6000000">
<Player machineIdentifier="machine-1" title="Living Room TV" product="Plex for LG" state="playing"/>
</Video>
</MediaContainer>`))
	}))
	defer srv.Close()

	p := &PlexClient{
		baseURL: srv.URL,
		token:   "tok",
		client:  http.DefaultClient,
	}
	sessions, err := p.ListPlaybackSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len sessions: got %d", len(sessions))
	}
	s := sessions[0]
	if s.PlayerName != "Living Room TV" || s.Title != "Test Movie" || s.PlayerState != "playing" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if s.ProgressPercent < 4.9 || s.ProgressPercent > 5.1 {
		t.Fatalf("progress: got %v", s.ProgressPercent)
	}
	if s.DisplayTitle() != "Test Movie (2020)" {
		t.Fatalf("DisplayTitle: %q", s.DisplayTitle())
	}
}
