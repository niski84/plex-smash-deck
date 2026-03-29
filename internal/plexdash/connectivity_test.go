package plexdash

import (
	"testing"
	"time"
)

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

func TestConnectivityBackoffCore(t *testing.T) {
	cases := []struct {
		streak int
		want   time.Duration
	}{
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{4, 32 * time.Second},
		{5, 64 * time.Second},
		{6, 90 * time.Second},
		{99, 90 * time.Second},
	}
	for _, tc := range cases {
		if got := connectivityBackoffCore(tc.streak); got != tc.want {
			t.Fatalf("streak %d: got %v want %v", tc.streak, got, tc.want)
		}
	}
	if got := connectivityBackoffCore(0); got != 4*time.Second {
		t.Fatalf("streak 0 normalized: got %v", got)
	}
}

func TestStreamProbeBackoffCore(t *testing.T) {
	if got := streamProbeBackoffCore(1); got != 20*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := streamProbeBackoffCore(4); got != 160*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := streamProbeBackoffCore(99); got != streamProbeBackoffMax {
		t.Fatalf("cap: got %v", got)
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
