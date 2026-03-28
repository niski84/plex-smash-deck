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
