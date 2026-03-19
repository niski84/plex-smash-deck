package plexdash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultSnapshotDir = "data/movie-snapshots"

// SnapshotMovie is the lightweight movie record stored inside each snapshot.
type SnapshotMovie struct {
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Rating    float64 `json:"rating,omitempty"`
	Genres    []string `json:"genres,omitempty"`
	Directors []string `json:"directors,omitempty"`
}

// Snapshot is a full point-in-time capture of the Plex library.
type Snapshot struct {
	ID         string          `json:"id"`
	CapturedAt time.Time       `json:"capturedAt"`
	Count      int             `json:"count"`
	Movies     []SnapshotMovie `json:"movies"`
}

// SnapshotMeta is the lightweight index entry (no movies list).
type SnapshotMeta struct {
	ID         string    `json:"id"`
	CapturedAt time.Time `json:"capturedAt"`
	Count      int       `json:"count"`
}

// SnapshotDiff is the result of comparing two snapshots.
type SnapshotDiff struct {
	From      SnapshotMeta    `json:"from"`
	To        SnapshotMeta    `json:"to"`
	Added     []SnapshotMovie `json:"added"`
	Removed   []SnapshotMovie `json:"removed"`
	NetChange int             `json:"netChange"`
}

func snapshotDir() string { return defaultSnapshotDir }

func snapshotFilePath(id string) string {
	return filepath.Join(snapshotDir(), "snapshot-"+id+".json")
}

func snapshotIndexPath() string {
	return filepath.Join(snapshotDir(), "index.json")
}

// ListSnapshots returns all snapshot metadata, newest first.
func ListSnapshots() ([]SnapshotMeta, error) {
	b, err := os.ReadFile(snapshotIndexPath())
	if os.IsNotExist(err) {
		return []SnapshotMeta{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []SnapshotMeta
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func saveSnapshotIndex(list []SnapshotMeta) error {
	if err := os.MkdirAll(snapshotDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := snapshotIndexPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, snapshotIndexPath())
}

// TakeSnapshot captures the current movie list and persists it to disk.
func TakeSnapshot(movies []Movie) (Snapshot, error) {
	id := time.Now().UTC().Format("20060102-150405")
	snap := Snapshot{
		ID:         id,
		CapturedAt: time.Now().UTC(),
		Count:      len(movies),
		Movies:     make([]SnapshotMovie, 0, len(movies)),
	}
	for _, m := range movies {
		sm := SnapshotMovie{
			Title:  m.Title,
			Year:   m.Year,
			Rating: m.Rating,
		}
		if len(m.Genres) > 0 {
			sm.Genres = append([]string(nil), m.Genres...)
		}
		if len(m.Directors) > 0 {
			sm.Directors = append([]string(nil), m.Directors...)
		}
		snap.Movies = append(snap.Movies, sm)
	}
	// Stable sort for predictable diffs.
	sort.Slice(snap.Movies, func(i, j int) bool {
		ti := strings.ToLower(snap.Movies[i].Title)
		tj := strings.ToLower(snap.Movies[j].Title)
		if ti != tj {
			return ti < tj
		}
		return snap.Movies[i].Year < snap.Movies[j].Year
	})

	if err := os.MkdirAll(snapshotDir(), 0o755); err != nil {
		return snap, err
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return snap, err
	}
	if err := os.WriteFile(snapshotFilePath(id), b, 0o644); err != nil {
		return snap, err
	}

	// Update index.
	list, _ := ListSnapshots()
	list = append([]SnapshotMeta{{ID: snap.ID, CapturedAt: snap.CapturedAt, Count: snap.Count}}, list...)
	if err := saveSnapshotIndex(list); err != nil {
		return snap, err
	}
	return snap, nil
}

// LoadSnapshot reads a single snapshot from disk by ID.
func LoadSnapshot(id string) (Snapshot, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/\\..") {
		return Snapshot{}, fmt.Errorf("invalid snapshot ID %q", id)
	}
	b, err := os.ReadFile(snapshotFilePath(id))
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot %q not found", id)
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// DiffSnapshots computes what was added and removed between two snapshots.
func DiffSnapshots(fromID, toID string) (SnapshotDiff, error) {
	from, err := LoadSnapshot(fromID)
	if err != nil {
		return SnapshotDiff{}, fmt.Errorf("loading 'from' snapshot: %w", err)
	}
	to, err := LoadSnapshot(toID)
	if err != nil {
		return SnapshotDiff{}, fmt.Errorf("loading 'to' snapshot: %w", err)
	}

	fromSet := make(map[string]struct{}, len(from.Movies))
	for _, m := range from.Movies {
		fromSet[snapshotKey(m)] = struct{}{}
	}
	toSet := make(map[string]struct{}, len(to.Movies))
	for _, m := range to.Movies {
		toSet[snapshotKey(m)] = struct{}{}
	}

	var added, removed []SnapshotMovie
	for _, m := range to.Movies {
		if _, ok := fromSet[snapshotKey(m)]; !ok {
			added = append(added, m)
		}
	}
	for _, m := range from.Movies {
		if _, ok := toSet[snapshotKey(m)]; !ok {
			removed = append(removed, m)
		}
	}

	return SnapshotDiff{
		From:      SnapshotMeta{ID: from.ID, CapturedAt: from.CapturedAt, Count: from.Count},
		To:        SnapshotMeta{ID: to.ID, CapturedAt: to.CapturedAt, Count: to.Count},
		Added:     nullSafeMovies(added),
		Removed:   nullSafeMovies(removed),
		NetChange: to.Count - from.Count,
	}, nil
}

// LatestDiff diffs the two most recent snapshots. Returns nil diff (no error)
// when fewer than two snapshots exist.
func LatestDiff() (*SnapshotDiff, error) {
	list, err := ListSnapshots()
	if err != nil {
		return nil, err
	}
	if len(list) < 2 {
		return nil, nil
	}
	// list is newest-first: [0]=newest, [1]=previous
	diff, err := DiffSnapshots(list[1].ID, list[0].ID)
	if err != nil {
		return nil, err
	}
	return &diff, nil
}

// MissingMovieEvent records a movie that vanished between two consecutive snapshots.
type MissingMovieEvent struct {
	Movie      SnapshotMovie `json:"movie"`
	LastSeenID string        `json:"lastSeenId"`   // snapshot where it was last present
	GoneID     string        `json:"goneId"`        // first snapshot it was absent from
	LastSeenAt time.Time     `json:"lastSeenAt"`
	GoneAt     time.Time     `json:"goneAt"`
}

// FindAllMissing scans every consecutive snapshot pair (chronological order)
// and returns movies that disappeared between any two consecutive snapshots.
// A movie is reported once per disappearance event. If it later reappears and
// disappears again, it will appear a second time.
func FindAllMissing() ([]MissingMovieEvent, error) {
	list, err := ListSnapshots()
	if err != nil {
		return nil, err
	}
	if len(list) < 2 {
		return []MissingMovieEvent{}, nil
	}

	// list is newest-first; reverse to chronological order for sequential scan.
	chrono := make([]SnapshotMeta, len(list))
	for i, m := range list {
		chrono[len(list)-1-i] = m
	}

	var events []MissingMovieEvent
	// Load snapshots lazily and walk consecutive pairs.
	for i := 0; i < len(chrono)-1; i++ {
		from, err := LoadSnapshot(chrono[i].ID)
		if err != nil {
			return nil, fmt.Errorf("loading snapshot %s: %w", chrono[i].ID, err)
		}
		to, err := LoadSnapshot(chrono[i+1].ID)
		if err != nil {
			return nil, fmt.Errorf("loading snapshot %s: %w", chrono[i+1].ID, err)
		}

		toSet := make(map[string]struct{}, len(to.Movies))
		for _, m := range to.Movies {
			toSet[snapshotKey(m)] = struct{}{}
		}
		for _, m := range from.Movies {
			if _, ok := toSet[snapshotKey(m)]; !ok {
				events = append(events, MissingMovieEvent{
					Movie:      m,
					LastSeenID: from.ID,
					GoneID:     to.ID,
					LastSeenAt: from.CapturedAt,
					GoneAt:     to.CapturedAt,
				})
			}
		}
	}

	if events == nil {
		return []MissingMovieEvent{}, nil
	}
	// Sort: most recently gone first so the UI shows the freshest issues at top.
	sort.Slice(events, func(i, j int) bool {
		return events[i].GoneAt.After(events[j].GoneAt)
	})
	return events, nil
}

func snapshotKey(m SnapshotMovie) string {
	return strings.ToLower(strings.TrimSpace(m.Title)) + "|" + fmt.Sprintf("%d", m.Year)
}

func nullSafeMovies(s []SnapshotMovie) []SnapshotMovie {
	if s == nil {
		return []SnapshotMovie{}
	}
	return s
}
