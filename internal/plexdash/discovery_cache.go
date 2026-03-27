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

// TTL fields are stored in JSON for debugging; reads ignore expiry until the user
// clears the cache (POST /api/discovery/cache/invalidate) or deletes files on disk.
const (
	discoveryCacheMovieDetailsTTL   = 60 * 24 * time.Hour
	discoveryCacheMovieCreditsTTL   = 60 * 24 * time.Hour
	discoveryCacheFilmographyTTL    = 60 * 24 * time.Hour
	discoveryCachePersonIDTTL       = 180 * 24 * time.Hour
	discoveryCacheStudioDiscoverTTL = 60 * 24 * time.Hour
	discoveryCacheCompanyIDTTL      = 180 * 24 * time.Hour
	discoveryCacheGenreMapTTL       = 365 * 24 * time.Hour
)

const discoveryCacheJSONVersion = 4

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

func (cachedEnvelope) expired() bool {
	return false // kept until POST /api/discovery/cache/invalidate or manual file delete
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
	Video       bool   `json:"video,omitempty"`
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
			Video: c.Video,
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
			Video: c.Video,
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
	OK               bool     `json:"ok"`
	Overview         string   `json:"overview"`
	Genres           []string `json:"genres"`
	VoteAverage      float64  `json:"voteAverage"`
	Runtime          int      `json:"runtime,omitempty"` // minutes; 0 = unknown
	PosterURL        string   `json:"posterUrl"`
	PosterPath       string   `json:"posterPath"` // raw TMDB poster_path (e.g. /x.jpg)
	OriginalLanguage string   `json:"originalLanguage,omitempty"`
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
		OK:               c.OK,
		Overview:         c.Overview,
		Genres:           append([]string(nil), c.Genres...),
		VoteAverage:      c.VoteAverage,
		Runtime:          c.Runtime,
		PosterURL:        c.PosterURL,
		PosterPath:       strings.TrimSpace(c.PosterPath),
		OriginalLanguage: c.OriginalLanguage,
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
		OK:               det.OK,
		Overview:         det.Overview,
		Genres:           append([]string(nil), det.Genres...),
		VoteAverage:      det.VoteAverage,
		Runtime:          det.Runtime,
		PosterURL:        det.PosterURL,
		PosterPath:       det.PosterPath,
		OriginalLanguage: det.OriginalLanguage,
	}
	_ = writeJSONAtomic(path, c)
}

// --- TMDB movie genre id → name (/genre/movie/list) ---

const genreMapMovieCacheFile = "genre-map-movie.json"

type cachedGenreMapMovie struct {
	cachedEnvelope
	Entries []genreMapEntry `json:"entries"`
}

type genreMapEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (d *diskDiscoveryCache) getGenreMapMovie() (map[int]string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(genreMapMovieCacheFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c cachedGenreMapMovie
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return nil, false
	}
	if c.expired() {
		return nil, false
	}
	m := make(map[int]string, len(c.Entries))
	for _, e := range c.Entries {
		m[e.ID] = e.Name
	}
	return m, true
}

func (d *diskDiscoveryCache) putGenreMapMovie(genres map[int]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	entries := make([]genreMapEntry, 0, len(genres))
	for id, name := range genres {
		entries = append(entries, genreMapEntry{ID: id, Name: name})
	}
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].ID < entries[i].ID {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	path := d.abs(genreMapMovieCacheFile)
	c := cachedGenreMapMovie{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCacheGenreMapTTL / time.Second),
		},
		Entries: entries,
	}
	_ = writeJSONAtomic(path, c)
}

// --- Company name → TMDB company id (search/company) ---

type cachedCompanyID struct {
	cachedEnvelope
	CompanyID    int    `json:"companyId"`
	ResolvedName string `json:"resolvedName"`
}

func companyNameCacheKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	h := sha256.Sum256([]byte(n))
	return "company-id-" + hex.EncodeToString(h[:12]) + ".json"
}

func (d *diskDiscoveryCache) getCompanyID(companyName string) (id int, resolvedName string, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(companyNameCacheKey(companyName))
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false
	}
	var c cachedCompanyID
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return 0, "", false
	}
	if c.expired() {
		return 0, "", false
	}
	return c.CompanyID, c.ResolvedName, true
}

func (d *diskDiscoveryCache) putCompanyID(companyName string, companyID int, resolvedName string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	path := d.abs(companyNameCacheKey(companyName))
	c := cachedCompanyID{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCacheCompanyIDTTL / time.Second),
		},
		CompanyID:    companyID,
		ResolvedName: resolvedName,
	}
	_ = writeJSONAtomic(path, c)
}

// --- Studio discover (/discover/movie?with_companies=) ---

type cachedStudioDiscover struct {
	cachedEnvelope
	Movies []tmdbDiscoverMovie `json:"movies"`
}

func studioDiscoverCacheFile(companyID, minYear, maxYear int) string {
	return fmt.Sprintf("studio-discover-%d-y%d-Y%d.json", companyID, minYear, maxYear)
}

func (d *diskDiscoveryCache) getStudioDiscover(companyID, minYear, maxYear int) ([]tmdbDiscoverMovie, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	path := d.abs(studioDiscoverCacheFile(companyID, minYear, maxYear))
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c cachedStudioDiscover
	if err := json.Unmarshal(b, &c); err != nil || c.Version != discoveryCacheJSONVersion {
		return nil, false
	}
	if c.expired() {
		return nil, false
	}
	out := make([]tmdbDiscoverMovie, len(c.Movies))
	copy(out, c.Movies)
	return out, true
}

func (d *diskDiscoveryCache) putStudioDiscover(companyID, minYear, maxYear int, movies []tmdbDiscoverMovie) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.ensureDir()
	path := d.abs(studioDiscoverCacheFile(companyID, minYear, maxYear))
	cp := make([]tmdbDiscoverMovie, len(movies))
	copy(cp, movies)
	for i := range cp {
		cp[i].Genres = append([]string(nil), cp[i].Genres...)
	}
	c := cachedStudioDiscover{
		cachedEnvelope: cachedEnvelope{
			Version:    discoveryCacheJSONVersion,
			CachedAt:   time.Now().UTC(),
			TTLSeconds: int64(discoveryCacheStudioDiscoverTTL / time.Second),
		},
		Movies: cp,
	}
	_ = writeJSONAtomic(path, c)
}
