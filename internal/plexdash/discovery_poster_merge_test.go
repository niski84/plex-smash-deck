package plexdash

import "testing"

func TestPosterPathPrefersCreditOverDetail(t *testing.T) {
	credit := tmdbCredit{PosterPath: "/credit.jpg"}
	det := fetchedMovieDetails{OK: true, PosterPath: "/detail.jpg", PosterURL: "https://image.tmdb.org/t/p/w185/detail.jpg"}
	raw := mergePosterRawPath(credit, det, true)
	if raw != "/credit.jpg" {
		t.Fatalf("want credit path first, got %q", raw)
	}
}

func TestPosterPathFallsBackToDetail(t *testing.T) {
	credit := tmdbCredit{PosterPath: ""}
	det := fetchedMovieDetails{OK: true, PosterPath: "/detail.jpg", PosterURL: "https://image.tmdb.org/t/p/w185/detail.jpg"}
	raw := mergePosterRawPath(credit, det, true)
	if raw != "/detail.jpg" {
		t.Fatalf("want detail path, got %q", raw)
	}
}
