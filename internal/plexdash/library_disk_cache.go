package plexdash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// libraryMemoryCacheFile is a full JSON copy of the in-memory Plex library list so a
// restarted process can serve /api/movies immediately without waiting on Plex.
const libraryMemoryCacheFile = "data/plex-library-memory.json"

type libraryMemoryPayload struct {
	LibraryKey string    `json:"libraryKey"`
	SavedAt    time.Time `json:"savedAt"`
	Movies     []Movie   `json:"movies"`
}

func libraryMemoryCachePath() string {
	return filepath.Clean(libraryMemoryCacheFile)
}

func (s *Server) saveLibraryMemoryToDisk(cfg Config, movies []Movie) {
	if len(movies) == 0 {
		return
	}
	lk := strings.TrimSpace(cfg.LibraryKey)
	if lk == "" {
		return
	}
	p := libraryMemoryCachePath()
	payload := libraryMemoryPayload{
		LibraryKey: lk,
		SavedAt:    time.Now().UTC(),
		Movies:     movies,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("[library-cache] marshal: %v\n", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fmt.Printf("[library-cache] mkdir: %v\n", err)
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		fmt.Printf("[library-cache] write: %v\n", err)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		fmt.Printf("[library-cache] rename: %v\n", err)
	}
}

// tryLoadLibraryMemoryFromDisk returns movies and savedAt when the file exists and matches cfg.LibraryKey.
func (s *Server) tryLoadLibraryMemoryFromDisk(cfg Config) (movies []Movie, savedAt time.Time, ok bool) {
	lk := strings.TrimSpace(cfg.LibraryKey)
	if lk == "" {
		return nil, time.Time{}, false
	}
	b, err := os.ReadFile(libraryMemoryCachePath())
	if err != nil {
		return nil, time.Time{}, false
	}
	var payload libraryMemoryPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, time.Time{}, false
	}
	if strings.TrimSpace(payload.LibraryKey) != lk {
		return nil, time.Time{}, false
	}
	if len(payload.Movies) == 0 {
		return nil, time.Time{}, false
	}
	sa := payload.SavedAt
	if sa.IsZero() {
		sa = time.Now()
	}
	return payload.Movies, sa, true
}

func (s *Server) removeLibraryMemoryDiskCache() {
	_ = os.Remove(libraryMemoryCachePath())
}
