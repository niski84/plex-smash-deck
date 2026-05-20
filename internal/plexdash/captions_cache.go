package plexdash

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CaptionsMeta is the sidecar JSON for a cached captions file.
type CaptionsMeta struct {
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Source    string `json:"source"`
	FetchedAt string `json:"fetchedAt"`
	ByteCount int    `json:"byteCount"`
}

// CaptionsCache is a tiny on-disk cache of plain-text captions keyed by IMDB id.
// Captions don't change once a movie ships, so there's no TTL — delete the file
// on disk to force a refresh.
type CaptionsCache struct {
	Dir string
}

// NewCaptionsCache returns a cache rooted at dir (created lazily on first Put).
func NewCaptionsCache(dir string) *CaptionsCache {
	return &CaptionsCache{Dir: filepath.Clean(dir)}
}

func (c *CaptionsCache) txtPath(imdbID string) string {
	return filepath.Join(c.Dir, sanitizeIMDb(imdbID)+".txt")
}

func (c *CaptionsCache) metaPath(imdbID string) string {
	return filepath.Join(c.Dir, sanitizeIMDb(imdbID)+".meta.json")
}

// Get returns cached captions and meta if present.
func (c *CaptionsCache) Get(imdbID string) (string, CaptionsMeta, bool) {
	imdbID = strings.TrimSpace(imdbID)
	if imdbID == "" {
		return "", CaptionsMeta{}, false
	}
	txt, err := os.ReadFile(c.txtPath(imdbID))
	if err != nil {
		return "", CaptionsMeta{}, false
	}
	var meta CaptionsMeta
	if mb, mErr := os.ReadFile(c.metaPath(imdbID)); mErr == nil {
		if jErr := json.Unmarshal(mb, &meta); jErr != nil {
			log.Printf("[captions] cache meta unmarshal %s: %v", imdbID, jErr)
		}
	}
	return string(txt), meta, true
}

// Put writes the captions text and sidecar meta atomically.
func (c *CaptionsCache) Put(imdbID, text string, meta CaptionsMeta) error {
	imdbID = strings.TrimSpace(imdbID)
	if imdbID == "" {
		return fmt.Errorf("captions cache put: empty imdbID")
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("captions cache mkdir: %w", err)
	}
	if err := atomicWriteFile(c.txtPath(imdbID), []byte(text), 0o644); err != nil {
		return fmt.Errorf("captions cache write txt: %w", err)
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("captions cache marshal meta: %w", err)
	}
	if err := atomicWriteFile(c.metaPath(imdbID), mb, 0o644); err != nil {
		return fmt.Errorf("captions cache write meta: %w", err)
	}
	return nil
}

// atomicWriteFile writes bytes to path via a tmp file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".captions-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// sanitizeIMDb keeps only alphanumeric chars so a malicious imdb id can't
// escape the cache directory.
var imdbSanitizeRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func sanitizeIMDb(s string) string {
	return imdbSanitizeRE.ReplaceAllString(strings.TrimSpace(s), "")
}

// CaptionsFetchedAt returns now in RFC3339 UTC — exposed so tests can stub via
// composition if needed.
func captionsNowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ── SRT → plain text ─────────────────────────────────────────────────────────

var (
	srtCueIndexRE     = regexp.MustCompile(`^\d+$`)
	srtTimestampRE    = regexp.MustCompile(`\d{2}:\d{2}:\d{2}[,\.]\d{3}\s*-->\s*\d{2}:\d{2}:\d{2}[,\.]\d{3}`)
	srtHTMLTagRE      = regexp.MustCompile(`<[^>]+>`)
	srtSpeakerLabelRE = regexp.MustCompile(`^[A-Z][A-Z\s]{1,15}:\s+`)
	srtBracketMusicRE = regexp.MustCompile(`(?i)[\[\(]\s*music[^\]\)]*[\]\)]`)
	srtWhitespaceRE   = regexp.MustCompile(`\s+`)
	// Music notes: a pair of music note symbols with anything between them is
	// almost always sung lyrics or a music cue and should be dropped wholesale.
	srtMusicNotePairRE = regexp.MustCompile(`[\x{266A}\x{266B}\x{266C}\x{266D}\x{266E}\x{266F}][^\x{266A}\x{266B}\x{266C}\x{266D}\x{266E}\x{266F}]*[\x{266A}\x{266B}\x{266C}\x{266D}\x{266E}\x{266F}]`)
	srtMusicNoteRE     = regexp.MustCompile(`[\x{266A}\x{266B}\x{266C}\x{266D}\x{266E}\x{266F}]`)
)

const srtBOMPrefix = "\ufeff"

// ParseSRTToPlainText converts SRT bytes into plain text.
//
//   - Cue-index lines (just digits) are dropped.
//   - Timestamp lines (HH:MM:SS,mmm --> ...) are dropped.
//   - HTML tags (<i>, <b>, <font ...>, ...) are stripped.
//   - Music symbols (♪, ♫, [music], (music)) are stripped.
//   - Speaker labels at the start of a line (JOHN:) are stripped.
//   - Empty lines are dropped.
//   - Cues are joined with newlines; runs of whitespace inside a cue collapse to a single space.
func ParseSRTToPlainText(srt []byte) string {
	if len(srt) == 0 {
		return ""
	}
	text := strings.TrimPrefix(string(srt), srtBOMPrefix)
	// Normalize line endings so split is consistent.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Cues are separated by blank lines.
	rawCues := strings.Split(text, "\n\n")
	out := make([]string, 0, len(rawCues))
	for _, cue := range rawCues {
		lines := strings.Split(cue, "\n")
		var cleaned []string
		for _, ln := range lines {
			s := strings.TrimSpace(ln)
			if s == "" {
				continue
			}
			if srtCueIndexRE.MatchString(s) {
				continue
			}
			if srtTimestampRE.MatchString(s) {
				continue
			}
			// Strip HTML tags first so speaker labels inside <i>JOHN:</i> still match.
			s = srtHTMLTagRE.ReplaceAllString(s, "")
			// Drop entire ♪ ... ♪ wrapped lyrics, then strip stray notes.
			s = srtMusicNotePairRE.ReplaceAllString(s, "")
			s = srtMusicNoteRE.ReplaceAllString(s, "")
			s = srtBracketMusicRE.ReplaceAllString(s, "")
			s = srtSpeakerLabelRE.ReplaceAllString(s, "")
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			s = srtWhitespaceRE.ReplaceAllString(s, " ")
			cleaned = append(cleaned, s)
		}
		if len(cleaned) == 0 {
			continue
		}
		out = append(out, strings.Join(cleaned, " "))
	}
	return strings.Join(out, "\n")
}
