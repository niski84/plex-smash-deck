package plexdash

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// discoveryDebugLogPath is where poster/filmography diagnostics are appended (UTF-8 text).
func discoveryDebugLogPath() string {
	return filepath.Clean("data/plexdash-discovery.log")
}

var discoveryDebugMu sync.Mutex

// DiscoveryDebugf appends one line with RFC3339 timestamp (safe for concurrent handlers).
func DiscoveryDebugf(format string, args ...any) {
	discoveryDebugMu.Lock()
	defer discoveryDebugMu.Unlock()
	path := discoveryDebugLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line := fmt.Sprintf(time.Now().UTC().Format(time.RFC3339)+" "+format+"\n", args...)
	_, _ = f.WriteString(line)
}
