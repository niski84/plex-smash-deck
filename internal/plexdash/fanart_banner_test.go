package plexdash

import "testing"

func TestBuildBannerQueue_filtersNoRatingKey(t *testing.T) {
	movies := []Movie{
		{RatingKey: "a", Title: "LowPlayed", ViewCount: 1, Rating: 8.0},
		{RatingKey: "b", Title: "HighRated", ViewCount: 5, Rating: 9.5},
		{RatingKey: "", Title: "NoKey", ViewCount: 99, Rating: 10.0},
	}
	got := buildBannerQueue(movies)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2 (NoKey must be excluded)", len(got))
	}
	for _, e := range got {
		if e.RatingKey == "" {
			t.Fatal("entry with empty RatingKey should be excluded")
		}
	}
}

func TestBuildBannerQueue_relatedMovieDetected(t *testing.T) {
	movies := []Movie{
		{RatingKey: "watched1", Title: "The Matrix", ViewCount: 5, Rating: 9.0,
			LastViewedAtEpoch: 1000, Actors: []string{"Keanu Reeves", "Laurence Fishburne"}},
		{RatingKey: "related1", Title: "John Wick", ViewCount: 2, Rating: 8.5,
			Actors: []string{"Keanu Reeves", "Ian McShane"}},
		{RatingKey: "unrelated", Title: "Some Other Film", ViewCount: 3, Rating: 7.0,
			Actors: []string{"Tom Hanks"}},
	}
	got := buildBannerQueue(movies)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	byKey := map[string]bannerEntry{}
	for _, e := range got {
		byKey[e.RatingKey] = e
	}
	rel := byKey["related1"]
	if rel.RelatedToTitle != "The Matrix" {
		t.Errorf("RelatedToTitle=%q want %q", rel.RelatedToTitle, "The Matrix")
	}
	if rel.RelatedViaActor != "Keanu Reeves" {
		t.Errorf("RelatedViaActor=%q want %q", rel.RelatedViaActor, "Keanu Reeves")
	}
	unrel := byKey["unrelated"]
	if unrel.RelatedToTitle != "" {
		t.Errorf("unrelated movie should have no RelatedToTitle, got %q", unrel.RelatedToTitle)
	}
}

func TestNormalizeBannerSchedule(t *testing.T) {
	if g := NormalizeBannerSchedule("1d", "1h"); g != "24h" {
		t.Fatalf("got %q", g)
	}
	if g := NormalizeBannerSchedule("bogus", "30m"); g != "30m" {
		t.Fatalf("got %q", g)
	}
	if g := NormalizeBannerSchedule("5m", "1h"); g != "5m" {
		t.Fatalf("got %q", g)
	}
}
