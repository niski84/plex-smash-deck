package plexdash

import (
	"math/rand"
	"testing"
)

func TestRatingKeysOrderedByViewCountShuffleTies_Buckets(t *testing.T) {
	movies := []Movie{
		{RatingKey: "high", ViewCount: 5},
		{RatingKey: "zero", ViewCount: 0},
		{RatingKey: "one", ViewCount: 1},
		{RatingKey: "alsozero", ViewCount: 0},
	}
	rng := rand.New(rand.NewSource(1))
	keys := ratingKeysOrderedByViewCountShuffleTies(movies, rng)
	if len(keys) != 4 {
		t.Fatalf("got %d keys", len(keys))
	}
	ix := func(k string) int {
		for i, x := range keys {
			if x == k {
				return i
			}
		}
		return -1
	}
	if ix("zero") >= 2 || ix("alsozero") >= 2 {
		t.Fatalf("view-count 0 items should be first two: %v", keys)
	}
	if ix("one") != 2 {
		t.Fatalf("view-count 1 should be third: %v", keys)
	}
	if ix("high") != 3 {
		t.Fatalf("highest view count should be last: %v", keys)
	}
}
