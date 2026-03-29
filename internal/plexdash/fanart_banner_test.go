package plexdash

import "testing"

func TestSortMoviesForFanartBanner_viewCountThenAddedAt(t *testing.T) {
	movies := []Movie{
		{RatingKey: "a", Title: "Old", ViewCount: 2, AddedAtEpoch: 100},
		{RatingKey: "b", Title: "NewSamePlays", ViewCount: 2, AddedAtEpoch: 500},
		{RatingKey: "c", Title: "MostPlayed", ViewCount: 10, AddedAtEpoch: 50},
		{RatingKey: "", Title: "NoKey", ViewCount: 99, AddedAtEpoch: 999},
	}
	got := sortMoviesForFanartBanner(movies)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].RatingKey != "c" || got[1].RatingKey != "b" || got[2].RatingKey != "a" {
		t.Fatalf("order wrong: %#v", got)
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
