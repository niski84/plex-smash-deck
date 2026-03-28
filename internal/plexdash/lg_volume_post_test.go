package plexdash

import (
	"bytes"
	"net/http"
	"testing"
)

func TestDecodeLgVolumePostJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw    string
		want   int
		wantErr bool
	}{
		{`{"level":42}`, 42, false},
		{`{"volume":7}`, 7, false},
		{`{"level":"33"}`, 0, true},
		{`{"level":0}`, 0, false},
		{`{"level":100}`, 100, false},
		{`{"level":150}`, 100, false},
		{`{"level":-5}`, 0, false},
		{`{"level":45.6}`, 46, false},
		{`{}`, 0, true},
	}
	for _, tc := range cases {
		req, err := http.NewRequest(http.MethodPost, "/api/lg/volume", bytes.NewBufferString(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		got, err := decodeLgVolumePostJSON(req)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %d", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %d want %d", tc.raw, got, tc.want)
		}
	}
}
