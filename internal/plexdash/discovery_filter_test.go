package plexdash

import "testing"

func TestTMDBPosterURLFromPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/7WsyChQLEftFiDOVTGkv3hFpyyt.jpg", "https://image.tmdb.org/t/p/w185/7WsyChQLEftFiDOVTGkv3hFpyyt.jpg"},
		{"7WsyChQLEftFiDOVTGkv3hFpyyt.jpg", "https://image.tmdb.org/t/p/w185/7WsyChQLEftFiDOVTGkv3hFpyyt.jpg"},
	}
	for _, tc := range cases {
		got := tmdbPosterURLFromPath(tc.in)
		if got != tc.want {
			t.Errorf("tmdbPosterURLFromPath(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidTMDBPosterFilePath(t *testing.T) {
	if !validTMDBPosterFilePath("/abc.jpg") {
		t.Fatal("expected /abc.jpg valid")
	}
	if validTMDBPosterFilePath("/../x.jpg") || validTMDBPosterFilePath("nolead.jpg") {
		t.Fatal("expected invalid")
	}
}

func TestExcludedFromDiscovery(t *testing.T) {
	cases := []struct {
		title    string
		overview string
		genres   []string
		wantExcl bool
	}{
		{"Some Film", "A thriller.", []string{"Thriller"}, false},
		{"The Story", "Plot.", []string{"Documentary"}, true},
		{"TV Movie Title", "", []string{"Drama", "TV Movie"}, true},
		{"Nightly News", "", []string{"News"}, true},
		{"Great Escape", "This documentary follows…", nil, true},
		{"Great Escape", "A prison break.", nil, false},
		{"The Making of Matrix", "", nil, true},
		{"The Remaking of Hope", "A drama.", nil, false},
		{"Matrix", "Behind the scenes look at stunts.", nil, true},
		{"Matrix", "Behind-the-scenes footage.", nil, true},
		{"Notadocumentarytitle", "ok", nil, false},
	}
	for _, tc := range cases {
		got := excludedFromDiscovery(tc.title, tc.overview, tc.genres)
		if got != tc.wantExcl {
			t.Errorf("%q / %q / %v: got %v want %v", tc.title, tc.overview, tc.genres, got, tc.wantExcl)
		}
	}
}
