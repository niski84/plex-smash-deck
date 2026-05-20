package plexdash

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSRTToPlainText(t *testing.T) {
	srt := []byte(`1
00:00:01,000 --> 00:00:03,000
<i>Hello</i> world

2
00:00:04,000 --> 00:00:06,000
♪ background music ♪
[music swells]

3
00:00:07,000 --> 00:00:09,000
JOHN: Wake up, Neo.
The matrix has you.

4
00:00:10,000 --> 00:00:12,000

5
00:00:13,000 --> 00:00:15,000
Follow   the   white   rabbit.
`)
	got := ParseSRTToPlainText(srt)
	want := "Hello world\nWake up, Neo. The matrix has you.\nFollow the white rabbit."
	if got != want {
		t.Fatalf("ParseSRTToPlainText mismatch\n got: %q\nwant: %q", got, want)
	}

	// Music symbols + bracket music alone produce an empty cue, which gets dropped.
	if strings.Contains(got, "music") {
		t.Fatalf("expected music line stripped, got: %q", got)
	}
	if strings.Contains(got, "♪") {
		t.Fatalf("expected music note symbol stripped, got: %q", got)
	}
}

func TestParseSRTToPlainText_Empty(t *testing.T) {
	if got := ParseSRTToPlainText(nil); got != "" {
		t.Fatalf("expected empty string for nil input, got %q", got)
	}
}

func TestCaptionsCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache := NewCaptionsCache(filepath.Join(dir, "captions"))

	imdb := "tt1234567"
	text := "line one\nline two"
	meta := CaptionsMeta{
		Title:     "The Matrix",
		Year:      1999,
		Source:    "opensubtitles",
		FetchedAt: "2026-04-26T00:00:00Z",
		ByteCount: len(text),
	}

	if _, _, ok := cache.Get(imdb); ok {
		t.Fatal("expected empty cache to miss")
	}
	if err := cache.Put(imdb, text, meta); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	gotText, gotMeta, ok := cache.Get(imdb)
	if !ok {
		t.Fatal("expected cache hit after Put")
	}
	if gotText != text {
		t.Fatalf("text mismatch: got %q want %q", gotText, text)
	}
	if gotMeta.Title != meta.Title || gotMeta.Year != meta.Year ||
		gotMeta.Source != meta.Source || gotMeta.ByteCount != meta.ByteCount ||
		gotMeta.FetchedAt != meta.FetchedAt {
		t.Fatalf("meta mismatch: got %+v want %+v", gotMeta, meta)
	}
}

func TestCaptionsCache_EmptyIMDb(t *testing.T) {
	cache := NewCaptionsCache(t.TempDir())
	if _, _, ok := cache.Get(""); ok {
		t.Fatal("expected miss for empty imdbID")
	}
	if err := cache.Put("", "x", CaptionsMeta{}); err == nil {
		t.Fatal("expected error for empty imdbID on Put")
	}
}
