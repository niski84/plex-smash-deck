package plexdash

import (
	"context"
	"testing"
)

func Test_discoveryEffectiveMinVote(t *testing.T) {
	if got := discoveryEffectiveMinVote(0); got != 0 {
		t.Fatalf("got %v want 0", got)
	}
	if got := discoveryEffectiveMinVote(7.0); got != 7.0 {
		t.Fatalf("got %v want 7.0", got)
	}
	if got := discoveryEffectiveMinVote(8.5); got != 8.5 {
		t.Fatalf("got %v want 8.5", got)
	}
}

func Test_movieMatchesGenreFilterOR(t *testing.T) {
	idToName := map[int]string{28: "Action", 35: "Comedy"}
	if !movieMatchesGenreFilterOR([]string{"Action", "Drama"}, []int{28}, idToName) {
		t.Fatal("want match on Action")
	}
	if movieMatchesGenreFilterOR([]string{"Drama"}, []int{28, 35}, idToName) {
		t.Fatal("want no match")
	}
	if !movieMatchesGenreFilterOR([]string{"x"}, nil, idToName) {
		t.Fatal("empty filter passes")
	}
}

func TestAnalyzeBrowse_requiresAtLeastOneYear(t *testing.T) {
	_, _, err := AnalyzeBrowse(context.Background(), Config{TMDBAPIKey: "x"}, nil, nil, 0, 0, 0, nil, false)
	if err == nil {
		t.Fatal("expected error when min and max year are both unset")
	}
}

func Test_findMatchingPlexMovie_yearTolerance(t *testing.T) {
	plex := []Movie{
		{RatingKey: "1", Title: "Example", Year: 2020, ViewCount: 0},
		{RatingKey: "2", Title: "Example", Year: 1999, ViewCount: 3},
	}
	m, ok := findMatchingPlexMovie(plex, "Example", 2021)
	if !ok || m.RatingKey != "1" {
		t.Fatalf("want 2020 match, got %+v ok=%v", m, ok)
	}
}

func TestPlexLibraryMatch_byImdb(t *testing.T) {
	plex := []Movie{{RatingKey: "1", Title: "F1: The Movie", Year: 2025, IMDbID: "tt9990001", ViewCount: 2}}
	ty := map[string][]int{normalizeTitle("F1: The Movie"): {2025}}
	byT := buildPlexTMDBIndex(plex)
	byI := buildPlexIMDBIndex(plex)
	ok, m := plexLibraryMatch(plex, ty, byT, byI, 0, "tt9990001", "F1", 2025)
	if !ok || m.RatingKey != "1" {
		t.Fatalf("want imdb match, got %+v ok=%v", m, ok)
	}
}

func TestPlexLibraryMatch_relaxedTitle(t *testing.T) {
	plex := []Movie{{RatingKey: "1", Title: "F1: The Movie", Year: 2025, ViewCount: 1}}
	ty := map[string][]int{normalizeTitle("F1: The Movie"): {2025}}
	byT := buildPlexTMDBIndex(plex)
	byI := buildPlexIMDBIndex(plex)
	ok, m := plexLibraryMatch(plex, ty, byT, byI, 0, "", "F1", 2025)
	if !ok || m.RatingKey != "1" {
		t.Fatalf("want relaxed title match, got %+v ok=%v", m, ok)
	}
}
