package plexdash

import "testing"

func TestParseOMDbIMDbRating(t *testing.T) {
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"8.4/10", 8.4, true},
		{"7", 7, true},
		{"N/A", 0, false},
		{"", 0, false},
	}
	for _, tc := range tests {
		v, ok := parseOMDbIMDbRating(tc.in)
		if ok != tc.ok || (ok && v != tc.want) {
			t.Fatalf("parseOMDbIMDbRating(%q) = (%v,%v) want (%v,%v)", tc.in, v, ok, tc.want, tc.ok)
		}
	}
}

func TestParseRottenTomatoesPercent(t *testing.T) {
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"85%", 8.5, true},
		{" 72 % ", 7.2, true},
		{"N/A", 0, false},
		{"", 0, false},
	}
	for _, tc := range tests {
		v, ok := parseRottenTomatoesPercent(tc.in)
		if ok != tc.ok || (ok && v != tc.want) {
			t.Fatalf("parseRottenTomatoesPercent(%q) = (%v,%v) want (%v,%v)", tc.in, v, ok, tc.want, tc.ok)
		}
	}
}

func TestParseMetacriticSlash(t *testing.T) {
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"75/100", 7.5, true},
		{"100/100", 10, true},
		{"N/A", 0, false},
	}
	for _, tc := range tests {
		v, ok := parseMetacriticSlash(tc.in)
		if ok != tc.ok || (ok && v != tc.want) {
			t.Fatalf("parseMetacriticSlash(%q) = (%v,%v) want (%v,%v)", tc.in, v, ok, tc.want, tc.ok)
		}
	}
}

func TestParseOMDbJSONToDetail(t *testing.T) {
	raw := `{
		"Response": "True",
		"imdbRating": "8.1/10",
		"Metascore": "75",
		"Ratings": [
			{"Source": "Internet Movie Database", "Value": "8.1/10"},
			{"Source": "Rotten Tomatoes", "Value": "90%"},
			{"Source": "Metacritic", "Value": "75/100"}
		]
	}`
	d := parseOMDbJSONToDetail("tt999", []byte(raw))
	if !d.OK {
		t.Fatalf("expected OK detail")
	}
	if len(d.Entries) < 3 {
		t.Fatalf("entries: %+v", d.Entries)
	}
	if averageScore10(d.Entries) <= 0 {
		t.Fatal("average")
	}
}
