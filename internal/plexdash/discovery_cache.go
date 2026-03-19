package plexdash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TTLs: movie metadata changes rarely; filmography should refresh for new roles.
const (
	discoveryCacheMovieDetailsTTL = 60 * 24 * time.Hour  // 60 days
	discoveryCacheMovieCreditsTTL = 60 * 24 * time.Hour
	discoveryCacheFilmographyTTL  = 7 * 24 * time.Hour // new credits appear over time
	discoveryCachePersonIDTTL     = 180 * 24 * time.Hour
)

const discoveryCacheJSONVersion = 3

func defaultDiscoveryCacheDir() string {
	return filepath.Clean("data/tmdb-discovery-cache")
}

// DiscoveryCacheStats is returned with filmography responses for observability.
type DiscoveryCacheStats struct {
	PersonIDHit        bool `json:"personIdHit"`
	FilmographyHit     bool `json:"filmographyHit"`
	MovieDetailsHits   int  `json:"movieDetailsHits"`
	MovieDetailsMisses int  `json:"movieDetailsMisses"`
	CreditsHits        int  `json:"creditsHits"`
	CreditsMisses      int  `json:"creditsMisses"`
}

// diskDiscoveryCache persists TMDB responses under data/tmdb-discovery-cache.
type diskDiscoveryCache struct {
	dir string
	mu  sync.RWMutex // writes serialize; reads can run in parallel
}

func newDiskDiscoveryCache(dir string) *diskDiscoveryCache {
	if strings.TrimSpace(dir) == "" {
		dir = defaultDiscoveryCacheDir()
	}
	return &diskDiscoveryCache{dir: filepath.Clean(dir)}
}

func (d *diskDiscoveryCache) ensureDir() error {
	return os.MkdirAll(d.dir, 0o755)
}

func (d *diskDiscoveryCache) abs(name string) string {
	return filepath.Join(d.dir, name)
}

func writeJSONAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type cachedEnvelope struct {
	Version   int       `json:"v"`
	CachedAt  time.Time `json:"cachedAt"`
	TTLSeconds int64    `json:"ttlSeconds"`
}

func (e cachedEnvelope) expired() bool {
	if e.CachedAt.IsZero() {
		return true
	}
	ttl := time.Duration(e.TTLSeconds) * time.Second
	if ttl <= 0 {
		return true
	}
	return time.Since(e.CachedAt) > ttl
}

// --- Person ID (search/person first result) ---

type cachedPersonID struct {
	cachedEnvelope
	PersonID int `json:"personId"`
}

func personNameCacheKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	h := sha256.Sum256([]byte(n))
	return "person-id-" + hex.EncodeToString(h[:12]) + ".json"
}

func (d *diskDiscoveryCache) getPersonID(personName string) (int, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(personNameCacheKey(personName))
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var c cachedPersonID
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return 0, false
	}
	if c.expired() {
		return 0, false
	}
	if c.PersonID <= 0 {
		return 0, false
	}
	return c.PersonID, true
}

func (d *diskDiscoveryCache) putPersonID(personName string, personID int) {
	if personID <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	path := d.abs(personNameCacheKey(personName))
	c := cachedPersonID{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCachePersonIDTTL / time.Second),
		},
		PersonID: personID,
	}
	_ = writeJSONAtomic(path, c)
}

// --- Filmography (parsed credits list) ---

type creditRecord struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Year        int    `json:"year"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	KnownFor    string `json:"knownFor"`
	Overview    string `json:"overview"`
	PosterPath  string `json:"posterPath"`
}

type cachedFilmography struct {
	cachedEnvelope
	Role    string         `json:"role"`
	Credits []creditRecord `json:"credits"`
}

func filmographyCacheFile(personID int, role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		r = "all"
	}
	// safe filename fragment
	r = strings.Map(func(r rune) rune {
		switch r {
		case 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '-', '_':
			return r
		default:
			return '_'
		}
	}, r)
	return fmt.Sprintf("filmography-%d-%s.json", personID, r)
}

func creditsToRecords(in []tmdbCredit) []creditRecord {
	out := make([]creditRecord, 0, len(in))
	for _, c := range in {
		out = append(out, creditRecord{
			ID: c.ID, Title: c.Title, Year: c.Year, ReleaseDate: c.ReleaseDate,
			KnownFor: c.KnownFor, Overview: c.Overview, PosterPath: c.PosterPath,
		})
	}
	return out
}

func recordsToCredits(in []creditRecord) []tmdbCredit {
	out := make([]tmdbCredit, 0, len(in))
	for _, c := range in {
		out = append(out, tmdbCredit{
			ID: c.ID, Title: c.Title, Year: c.Year, ReleaseDate: c.ReleaseDate,
			KnownFor: c.KnownFor, Overview: c.Overview, PosterPath: c.PosterPath,
		})
	}
	return out
}

func (d *diskDiscoveryCache) getFilmography(personID int, role string) ([]tmdbCredit, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(filmographyCacheFile(personID, role))
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c cachedFilmography
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return nil, false
	}
	if c.expired() {
		return nil, false
	}
	return recordsToCredits(c.Credits), true
}

func (d *diskDiscoveryCache) putFilmography(personID int, role string, credits []tmdbCredit) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	path := d.abs(filmographyCacheFile(personID, role))
	c := cachedFilmography{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCacheFilmographyTTL / time.Second),
		},
		Role:    role,
		Credits: creditsToRecords(credits),
	}
	_ = writeJSONAtomic(path, c)
}

// --- Movie credits (/movie/{id}/credits) ---

type cachedMovieCredits struct {
	cachedEnvelope
	Directors []string `json:"directors"`
	Actors    []string `json:"actors"`
}

func movieCreditsCacheFile(movieID int) string {
	return fmt.Sprintf("movie-credits-%d.json", movieID)
}

func (d *diskDiscoveryCache) getMovieCredits(movieID int) (tmdbMovieCredits, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(movieCreditsCacheFile(movieID))
	b, err := os.ReadFile(path)
	if err != nil {
		return tmdbMovieCredits{}, false
	}
	var c cachedMovieCredits
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return tmdbMovieCredits{}, false
	}
	if c.expired() {
		return tmdbMovieCredits{}, false
	}
	return tmdbMovieCredits{Directors: c.Directors, Actors: c.Actors}, true
}

func (d *diskDiscoveryCache) putMovieCredits(movieID int, mc tmdbMovieCredits) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	path := d.abs(movieCreditsCacheFile(movieID))
	c := cachedMovieCredits{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCacheMovieCreditsTTL / time.Second),
		},
		Directors: append([]string(nil), mc.Directors...),
		Actors:    append([]string(nil), mc.Actors...),
	}
	_ = writeJSONAtomic(path, c)
}

// --- Movie details (/movie/{id}) ---

type cachedMovieDetails struct {
	cachedEnvelope
	OK          bool     `json:"ok"`
	Overview    string   `json:"overview"`
	Genres      []string `json:"genres"`
	VoteAverage float64  `json:"voteAverage"`
	Runtime     int      `json:"runtime,omitempty"` // minutes; 0 = unknown
	PosterURL   string   `json:"posterUrl"`
	PosterPath  string   `json:"posterPath"` // raw TMDB poster_path (e.g. /x.jpg)
}

func movieDetailsCacheFile(movieID int) string {
	return fmt.Sprintf("movie-details-%d.json", movieID)
}

func (d *diskDiscoveryCache) getMovieDetails(movieID int) (fetchedMovieDetails, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(movieDetailsCacheFile(movieID))
	b, err := os.ReadFile(path)
	if err != nil {
		return fetchedMovieDetails{}, false
	}
	var c cachedMovieDetails
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return fetchedMovieDetails{}, false
	}
	if c.expired() {
		return fetchedMovieDetails{}, false
	}
	return fetchedMovieDetails{
		OK:          c.OK,
		Overview:    c.Overview,
		Genres:      append([]string(nil), c.Genres...),
		VoteAverage: c.VoteAverage,
		Runtime:     c.Runtime,
		PosterURL:   c.PosterURL,
		PosterPath:  strings.TrimSpace(c.PosterPath),
	}, true
}

func (d *diskDiscoveryCache) putMovieDetails(movieID int, det fetchedMovieDetails) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	path := d.abs(movieDetailsCacheFile(movieID))
	c := cachedMovieDetails{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCacheMovieDetailsTTL / time.Second),
		},
		OK:          det.OK,
		Overview:    det.Overview,
		Genres:      append([]string(nil), det.Genres...),
		VoteAverage: det.VoteAverage,
		Runtime:     det.Runtime,
		PosterURL:   det.PosterURL,
		PosterPath:  det.PosterPath,
	}
	_ = writeJSONAtomic(path, c)
}
