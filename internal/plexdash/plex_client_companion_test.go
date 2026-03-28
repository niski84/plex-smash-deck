package plexdash

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPlaybackControl_buildsRequest(t *testing.T) {
	var gotPath, gotQuery string
	var gotTarget string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotTarget = r.Header.Get("X-Plex-Target-Client-Identifier")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := &PlexClient{
		baseURL: srv.URL,
		token:   "test-token",
		client:  srv.Client(),
	}
	pl := Player{
		Name:             "Test Player",
		ClientIdentifier: "machine-uuid-1",
		URI:              srv.URL,
	}
	ctx := context.Background()
	if err := p.SendPlaybackControl(ctx, pl, "pause", 0); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/player/playback/pause" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotTarget != "machine-uuid-1" {
		t.Fatalf("target header: got %q", gotTarget)
	}
	if !strings.Contains(gotQuery, "X-Plex-Token=test-token") || !strings.Contains(gotQuery, "type=video") || !strings.Contains(gotQuery, "commandID=0") {
		t.Fatalf("query: %q", gotQuery)
	}

	if err := p.SendPlaybackControl(ctx, pl, "seekTo", 120000); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/player/playback/seekTo" {
		t.Fatalf("seek path: got %q", gotPath)
	}
	if !strings.Contains(gotQuery, "offset=120000") {
		t.Fatalf("seek query: %q", gotQuery)
	}
}

func TestSelectPlayerForCompanion_prefersNonSSAP(t *testing.T) {
	players := []Player{
		{Name: "Living Room", ClientIdentifier: "lgtv-ssap-static", URI: "ssap://192.168.1.5", Product: "Plex for LG"},
		{Name: "Living Room", ClientIdentifier: "real-id", URI: "https://plex.tv", Product: "Plex for LG"},
	}
	p, err := SelectPlayerForCompanion(players, "living")
	if err != nil {
		t.Fatal(err)
	}
	if p.ClientIdentifier != "real-id" {
		t.Fatalf("got %+v", p)
	}
}

func TestSelectPlayerForCompanion_ssapOnlyErrors(t *testing.T) {
	players := []Player{
		{Name: "LG TV (SSAP)", ClientIdentifier: "lgtv-ssap-static", URI: "ssap://192.168.1.5"},
	}
	_, err := SelectPlayerForCompanion(players, "ssap")
	if err == nil {
		t.Fatal("expected error")
	}
}
