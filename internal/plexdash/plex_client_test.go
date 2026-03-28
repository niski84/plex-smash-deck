package plexdash

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSortMoviesDefaultViewWithRand_tiersAndRecency(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	movies := []Movie{
		{RatingKey: "a", Title: "Old Classic", Year: 1960, Rating: 8.5},
		{RatingKey: "b", Title: "New Mid", Year: 2022, Rating: 7.0},
		{RatingKey: "c", Title: "Recent Great", Year: 2024, Rating: 8.1},
		{RatingKey: "d", Title: "Recent Ok", Year: 2024, Rating: 6.0},
	}
	SortMoviesDefaultViewWithRand(movies, rng)
	if got := movies[0].RatingKey + movies[1].RatingKey + movies[2].RatingKey + movies[3].RatingKey; got != "cadb" {
		t.Fatalf("order want cadb, got %q (%+v)", got, movies)
	}
}

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

func Test_movieCountFromPlexLibraryAllXML(t *testing.T) {
	t.Run("totalSize with paginated page", func(t *testing.T) {
		xmlBody := `<MediaContainer size="1" totalSize="42"><Video ratingKey="1"/></MediaContainer>`
		n, err := movieCountFromPlexLibraryAllXML([]byte(xmlBody))
		if err != nil {
			t.Fatal(err)
		}
		if n != 42 {
			t.Fatalf("got %d", n)
		}
	})
	t.Run("non paginated size only", func(t *testing.T) {
		xmlBody := `<MediaContainer size="3"><Video/><Video/><Video/></MediaContainer>`
		n, err := movieCountFromPlexLibraryAllXML([]byte(xmlBody))
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Fatalf("got %d", n)
		}
	})
	t.Run("paginated without totalSize", func(t *testing.T) {
		xmlBody := `<MediaContainer size="1"><Video ratingKey="1"/></MediaContainer>`
		_, err := movieCountFromPlexLibraryAllXML([]byte(xmlBody))
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, errPlexTotalSizeMissing) {
			t.Fatalf("expected errPlexTotalSizeMissing, got %v", err)
		}
	})
}

func TestFetchNewMoviesFromRecentlyAdded_skipsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/library/sections/1/recentlyAdded" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<MediaContainer size="2">
  <Video ratingKey="old1" title="Old" year="2020"/>
  <Video ratingKey="new1" title="New" year="2024"/>
</MediaContainer>`))
	}))
	defer srv.Close()

	p := &PlexClient{baseURL: srv.URL, token: "t", client: srv.Client()}
	existing := map[string]struct{}{"old1": {}}
	out, err := p.FetchNewMoviesFromRecentlyAdded(context.Background(), "1", existing, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].RatingKey != "new1" {
		t.Fatalf("got %+v", out)
	}
}

func TestLibraryMovieTotalCount_httptest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/library/sections/2/all" {
			t.Fatalf("path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("X-Plex-Container-Start") != "0" || q.Get("X-Plex-Container-Size") != "1" {
			t.Fatalf("pagination query: %v", q)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<MediaContainer size="1" totalSize="7"><Video ratingKey="1"/></MediaContainer>`))
	}))
	defer srv.Close()

	p := &PlexClient{
		baseURL: srv.URL,
		token:   "tok",
		client:  srv.Client(),
	}
	n, err := p.LibraryMovieTotalCount(context.Background(), "2")
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("got %d", n)
	}
}

func TestParseTMDBIDFromPlexGuid(t *testing.T) {
	tests := []struct {
		guid string
		want int
	}{
		{"", 0},
		{"tmdb://9517", 9517},
		{"plex://movie/guid/tmdb://9517", 9517},
		{"com.plexapp.agents.themoviedb://9517?lang=en", 9517},
	}
	for _, tc := range tests {
		if got := parseTMDBIDFromPlexGuid(tc.guid); got != tc.want {
			t.Errorf("parseTMDBIDFromPlexGuid(%q) = %d want %d", tc.guid, got, tc.want)
		}
	}
}
