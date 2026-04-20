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

// snapshotDir returns the directory for index.json and snapshot-*.json files.
// Set PLEXDASH_SNAPSHOT_DIR to an absolute path to share history across git worktrees
// (snapshots are gitignored and default to per-checkout data/movie-snapshots).
func snapshotDir() string {
	if d := strings.TrimSpace(os.Getenv("PLEXDASH_SNAPSHOT_DIR")); d != "" {
		return filepath.Clean(d)
	}
	return defaultSnapshotDir
}

// SnapshotMovie is the lightweight movie record stored inside each snapshot.
type SnapshotMovie struct {
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Rating    float64  `json:"rating,omitempty"`
	Studio    string   `json:"studio,omitempty"`
	Genres    []string `json:"genres,omitempty"`
	Directors []string `json:"directors,omitempty"`
	Actors    []string `json:"actors,omitempty"`
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
	NoChange   bool      `json:"noChange,omitempty"` // true = scheduler ran but library was identical; no snapshot file exists
}

// YearDrift records a movie whose metadata year changed between two snapshots.
// The title and director(s) match; only the year differs.
type YearDrift struct {
	Before SnapshotMovie `json:"before"`
	After  SnapshotMovie `json:"after"`
}

// YearDriftEvent is a YearDrift tied to specific snapshot IDs / times.
type YearDriftEvent struct {
	Before     SnapshotMovie `json:"before"`
	After      SnapshotMovie `json:"after"`
	LastSeenID string        `json:"lastSeenId"`
	ChangedID  string        `json:"changedId"`
	LastSeenAt time.Time     `json:"lastSeenAt"`
	ChangedAt  time.Time     `json:"changedAt"`
}

// SnapshotDiff is the result of comparing two snapshots.
type SnapshotDiff struct {
	From      SnapshotMeta    `json:"from"`
	To        SnapshotMeta    `json:"to"`
	Added     []SnapshotMovie `json:"added"`
	Removed   []SnapshotMovie `json:"removed"`
	YearDrift []YearDrift     `json:"yearDrift"`
	NetChange int             `json:"netChange"`
}

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
			Studio: m.Studio,
		}
		if len(m.Genres) > 0 {
			sm.Genres = append([]string(nil), m.Genres...)
		}
		if len(m.Directors) > 0 {
			sm.Directors = append([]string(nil), m.Directors...)
		}
		if len(m.Actors) > 0 {
			sm.Actors = append([]string(nil), m.Actors...)
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

	var rawAdded, rawRemoved []SnapshotMovie
	for _, m := range to.Movies {
		if _, ok := fromSet[snapshotKey(m)]; !ok {
			rawAdded = append(rawAdded, m)
		}
	}
	for _, m := range from.Movies {
		if _, ok := toSet[snapshotKey(m)]; !ok {
			rawRemoved = append(rawRemoved, m)
		}
	}

	drifts, added, removed := reconcileYearDrift(rawAdded, rawRemoved)

	return SnapshotDiff{
		From:      SnapshotMeta{ID: from.ID, CapturedAt: from.CapturedAt, Count: from.Count},
		To:        SnapshotMeta{ID: to.ID, CapturedAt: to.CapturedAt, Count: to.Count},
		Added:     added,
		Removed:   removed,
		YearDrift: nullSafeYearDrift(drifts),
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

// FindAllMissingAndDrift scans every consecutive snapshot pair (chronological
// order) and returns:
//   - movies that truly disappeared (missing)
//   - movies whose metadata year changed (yearDrift)
//
// Year-drift pairs are excluded from the missing list so they don't trigger
// false removal alerts.
func FindAllMissingAndDrift() ([]MissingMovieEvent, []YearDriftEvent, error) {
	list, err := ListSnapshots()
	if err != nil {
		return nil, nil, err
	}
	if len(list) < 2 {
		return []MissingMovieEvent{}, []YearDriftEvent{}, nil
	}

	// list is newest-first; reverse to chronological order for sequential scan.
	chrono := make([]SnapshotMeta, len(list))
	for i, m := range list {
		chrono[len(list)-1-i] = m
	}

	var missing []MissingMovieEvent
	var driftEvents []YearDriftEvent

	for i := 0; i < len(chrono)-1; i++ {
		from, err := LoadSnapshot(chrono[i].ID)
		if err != nil {
			return nil, nil, fmt.Errorf("loading snapshot %s: %w", chrono[i].ID, err)
		}
		to, err := LoadSnapshot(chrono[i+1].ID)
		if err != nil {
			return nil, nil, fmt.Errorf("loading snapshot %s: %w", chrono[i+1].ID, err)
		}

		toSet := make(map[string]struct{}, len(to.Movies))
		for _, m := range to.Movies {
			toSet[snapshotKey(m)] = struct{}{}
		}

		// Collect raw disappearances then reconcile year drift.
		var rawRemoved, rawAdded []SnapshotMovie
		for _, m := range from.Movies {
			if _, ok := toSet[snapshotKey(m)]; !ok {
				rawRemoved = append(rawRemoved, m)
			}
		}
		fromSet := make(map[string]struct{}, len(from.Movies))
		for _, m := range from.Movies {
			fromSet[snapshotKey(m)] = struct{}{}
		}
		for _, m := range to.Movies {
			if _, ok := fromSet[snapshotKey(m)]; !ok {
				rawAdded = append(rawAdded, m)
			}
		}

		drifts, _, trueRemoved := reconcileYearDrift(rawAdded, rawRemoved)

		for _, d := range drifts {
			driftEvents = append(driftEvents, YearDriftEvent{
				Before:     d.Before,
				After:      d.After,
				LastSeenID: from.ID,
				ChangedID:  to.ID,
				LastSeenAt: from.CapturedAt,
				ChangedAt:  to.CapturedAt,
			})
		}
		for _, m := range trueRemoved {
			missing = append(missing, MissingMovieEvent{
				Movie:      m,
				LastSeenID: from.ID,
				GoneID:     to.ID,
				LastSeenAt: from.CapturedAt,
				GoneAt:     to.CapturedAt,
			})
		}
	}

	if missing == nil {
		missing = []MissingMovieEvent{}
	}
	if driftEvents == nil {
		driftEvents = []YearDriftEvent{}
	}

	sort.Slice(missing, func(i, j int) bool {
		return missing[i].GoneAt.After(missing[j].GoneAt)
	})
	sort.Slice(driftEvents, func(i, j int) bool {
		return driftEvents[i].ChangedAt.After(driftEvents[j].ChangedAt)
	})
	return missing, driftEvents, nil
}

// TakeSnapshotDedup takes a snapshot and immediately diffs it against the
// previous one. If nothing has changed (same movies by title+year), the new
// file is deleted and a lightweight no-change marker is written to the index
// so the UI can show that the scheduler ran but found nothing new.
// Returns a meta with NoChange=true when the snapshot was discarded.
func TakeSnapshotDedup(movies []Movie) (*SnapshotMeta, error) {
	snap, err := TakeSnapshot(movies)
	if err != nil {
		return nil, err
	}

	list, err := ListSnapshots()
	if err != nil {
		meta := SnapshotMeta{ID: snap.ID, CapturedAt: snap.CapturedAt, Count: snap.Count}
		return &meta, nil
	}

	// Need at least two entries (new + previous) to compare.
	if len(list) < 2 {
		meta := SnapshotMeta{ID: snap.ID, CapturedAt: snap.CapturedAt, Count: snap.Count}
		return &meta, nil
	}

	// list[0] is the just-written snapshot; list[1] is the previous one.
	diff, err := DiffSnapshots(list[1].ID, list[0].ID)
	if err != nil {
		meta := SnapshotMeta{ID: snap.ID, CapturedAt: snap.CapturedAt, Count: snap.Count}
		return &meta, nil
	}

	if len(diff.Added) == 0 && len(diff.Removed) == 0 {
		// No change — delete the snapshot file and replace the index entry with
		// a lightweight no-change marker so the UI knows the scheduler ran.
		_ = os.Remove(snapshotFilePath(snap.ID))
		marker := SnapshotMeta{
			ID:         snap.ID,
			CapturedAt: snap.CapturedAt,
			Count:      snap.Count,
			NoChange:   true,
		}
		pruned := make([]SnapshotMeta, 0, len(list))
		pruned = append(pruned, marker)
		for _, m := range list {
			if m.ID != snap.ID {
				pruned = append(pruned, m)
			}
		}
		_ = saveSnapshotIndex(pruned)
		fmt.Printf("[snapshot] daily snapshot %s had no diff — marker written\n", snap.ID)
		return &marker, nil
	}

	fmt.Printf("[snapshot] daily snapshot %s: +%d added, -%d removed\n", snap.ID, len(diff.Added), len(diff.Removed))
	meta := SnapshotMeta{ID: snap.ID, CapturedAt: snap.CapturedAt, Count: snap.Count}
	return &meta, nil
}

func snapshotKey(m SnapshotMovie) string {
	return strings.ToLower(strings.TrimSpace(m.Title)) + "|" + fmt.Sprintf("%d", m.Year)
}

// directorsOverlap returns true if the two director lists share at least one
// name. If either list is empty we assume overlap — title match alone is
// sufficient when director data isn't available.
func directorsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, da := range a {
		for _, db := range b {
			if strings.EqualFold(strings.TrimSpace(da), strings.TrimSpace(db)) {
				return true
			}
		}
	}
	return false
}

// reconcileYearDrift separates year-drift pairs from plain added/removed lists.
// A year drift is: same title (case-insensitive), different year, director overlap.
func reconcileYearDrift(added, removed []SnapshotMovie) (drifts []YearDrift, filteredAdded, filteredRemoved []SnapshotMovie) {
	type entry struct {
		idx int
		m   SnapshotMovie
	}
	byTitle := make(map[string][]entry, len(removed))
	for i, m := range removed {
		k := strings.ToLower(strings.TrimSpace(m.Title))
		byTitle[k] = append(byTitle[k], entry{i, m})
	}

	usedRemoved := make(map[int]bool)
	usedAdded := make(map[int]bool)

	for ai, a := range added {
		k := strings.ToLower(strings.TrimSpace(a.Title))
		for _, cand := range byTitle[k] {
			if usedRemoved[cand.idx] {
				continue
			}
			if cand.m.Year == a.Year {
				continue // same year — not a drift
			}
			if !directorsOverlap(a.Directors, cand.m.Directors) {
				continue
			}
			drifts = append(drifts, YearDrift{Before: cand.m, After: a})
			usedRemoved[cand.idx] = true
			usedAdded[ai] = true
			break
		}
	}

	for i, m := range added {
		if !usedAdded[i] {
			filteredAdded = append(filteredAdded, m)
		}
	}
	for i, m := range removed {
		if !usedRemoved[i] {
			filteredRemoved = append(filteredRemoved, m)
		}
	}
	return drifts, nullSafeMovies(filteredAdded), nullSafeMovies(filteredRemoved)
}

func nullSafeMovies(s []SnapshotMovie) []SnapshotMovie {
	if s == nil {
		return []SnapshotMovie{}
	}
	return s
}

func nullSafeYearDrift(s []YearDrift) []YearDrift {
	if s == nil {
		return []YearDrift{}
	}
	return s
}
