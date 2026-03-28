package plexdash

import "testing"

func TestPickMovieForStreamProbe(t *testing.T) {
	movies := []Movie{
		{Title: "Big", PartKey: "/library/parts/2/file.mkv", PartSize: 20e9},
		{Title: "Small", PartKey: "/library/parts/1/file.mkv", PartSize: 500e6},
		{Title: "NoPart", PartKey: ""},
	}
	m, ok := pickMovieForStreamProbe(movies)
	if !ok {
		t.Fatal("expected a movie")
	}
	if m.Title != "Small" {
		t.Fatalf("want smallest PartSize, got %q", m.Title)
	}
}

func TestSummarizeConnectivity_StreamWarn(t *testing.T) {
	checks := []ConnectivityCheck{
		{ID: "internet", Level: "ok", Message: "ok"},
		{ID: "plex", Level: "ok", Message: "ok"},
	}
	stream := PlexStreamMetrics{Level: "warn", Message: "slow"}
	o, s := summarizeConnectivity(checks, stream)
	if o != "warn" {
		t.Fatalf("overall: %q", o)
	}
	if s == "" {
		t.Fatal("empty summary")
	}
}
