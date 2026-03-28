package plexdash

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port               string
	PlexBaseURL        string
	PlexToken          string
	LibraryKey         string
	TargetClientName   string
	AutoTargetDetected bool
	AppDisplayName     string
	HeroBannerURL      string
	HeroBannerHeight   int
	HeroBannerHidden   bool
	TMDBAPIKey         string
	TMDBReadToken      string
	RadarrEnabled      bool
	RadarrURL          string
	RadarrAPIKey       string
	RadarrRootFolder   string
	RadarrProfileID    int
	// LG webOS TV direct control via SSAP WebSocket (port 3001).
	// LGTVAddr is the TV's local IP (e.g. "192.168.4.153").
	// LGTVClientKey is the key obtained during one-time pairing.
	LGTVAddr      string
	LGTVClientKey string

	// Snapshot schedule. SnapshotDisabled=false (zero) means enabled — correct
	// default without needing special handling. SnapshotHour is 0–23 UTC;
	// zero value means midnight, which is a valid choice.
	SnapshotDisabled bool
	SnapshotHour     int
}

func LoadConfig() (Config, error) {
	if p := findDotEnvPath(); p != "" {
		if err := loadDotEnv(p); err != nil {
			fmt.Fprintf(os.Stderr, "[plexdash] warning: could not read .env from %s: %v\n", p, err)
		}
	}

	cfg := Config{
		Port:               getenv("PORT", "8081"),
		PlexBaseURL:        strings.TrimRight(os.Getenv("PLEX_BASE_URL"), "/"),
		PlexToken:          os.Getenv("PLEX_TOKEN"),
		LibraryKey:         getenv("PLEX_LIBRARY_KEY", "1"),
		TargetClientName:   getenv("PLEX_TARGET_CLIENT_NAME", "Living Room"),
		AutoTargetDetected: getenv("PLEX_AUTO_TARGET_DETECTED", "") == "1",
		AppDisplayName:     getenv("APP_DISPLAY_NAME", "plex-smash-deck"),
		HeroBannerURL:      os.Getenv("HERO_BANNER_URL"),
		HeroBannerHeight:   getenvInt("HERO_BANNER_HEIGHT", 140),
		HeroBannerHidden:   getenv("HERO_BANNER_HIDDEN", "") == "1",
		TMDBAPIKey:         os.Getenv("TMDB_API_KEY"),
		TMDBReadToken:      os.Getenv("TMDB_READ_ACCESS_TOKEN"),
		RadarrEnabled:      getenv("RADARR_ENABLED", "") == "1",
		RadarrURL:          strings.TrimRight(os.Getenv("RADARR_URL"), "/"),
		RadarrAPIKey:       os.Getenv("RADARR_API_KEY"),
		RadarrRootFolder:   os.Getenv("RADARR_ROOT_FOLDER"),
		RadarrProfileID:    getenvInt("RADARR_PROFILE_ID", 1),
		LGTVAddr:           os.Getenv("LGTV_ADDR"),
		LGTVClientKey:      os.Getenv("LGTV_CLIENT_KEY"),
	}

	// Source from .env first, then fill missing values from persisted settings.
	stored, err := loadPersistedConfig(defaultSettingsPath())
	if err == nil {
		mergeMissingConfig(&cfg, stored)
	}
	if strings.TrimSpace(cfg.AppDisplayName) == "" {
		cfg.AppDisplayName = "plex-smash-deck"
	}
	if !cfg.RadarrEnabled &&
		(strings.TrimSpace(cfg.RadarrURL) != "" ||
			strings.TrimSpace(cfg.RadarrAPIKey) != "" ||
			strings.TrimSpace(cfg.RadarrRootFolder) != "") {
		// Backward-compat: older saved settings had no explicit toggle.
		cfg.RadarrEnabled = true
	}
	if cfg.HeroBannerHeight <= 0 {
		cfg.HeroBannerHeight = 140
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	val := os.Getenv(key)
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

// findDotEnvPath locates a .env file so config works when cwd is not the repo root
// (IDE, systemd, worktrees without a copied .env). Override with PLEX_DASHBOARD_ENV_FILE.
func findDotEnvPath() string {
	if p := strings.TrimSpace(os.Getenv("PLEX_DASHBOARD_ENV_FILE")); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := filepath.Clean(wd); ; {
			cand := filepath.Join(dir, ".env")
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Clean(filepath.Dir(exe))
		cand := filepath.Join(exeDir, ".env")
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	return ""
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		// Fill missing or blank env so .env wins over empty exports from the parent shell.
		if cur, exists := os.LookupEnv(key); !exists || strings.TrimSpace(cur) == "" {
			_ = os.Setenv(key, val)
		}
	}

	return scanner.Err()
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil {
		return fallback
	}
	return value
}

func defaultSettingsPath() string {
	return filepath.Clean("data/plexdash-settings.json")
}

func loadPersistedConfig(path string) (Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SavePersistedConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

func mergeMissingConfig(dst *Config, src Config) {
	if dst.PlexBaseURL == "" {
		dst.PlexBaseURL = src.PlexBaseURL
	}
	if dst.PlexToken == "" {
		dst.PlexToken = src.PlexToken
	}
	if dst.LibraryKey == "" {
		dst.LibraryKey = src.LibraryKey
	}
	if dst.TargetClientName == "" {
		dst.TargetClientName = src.TargetClientName
	}
	dst.AutoTargetDetected = src.AutoTargetDetected
	if dst.AppDisplayName == "" {
		dst.AppDisplayName = src.AppDisplayName
	}
	if dst.HeroBannerURL == "" {
		dst.HeroBannerURL = src.HeroBannerURL
	}
	if dst.HeroBannerHeight == 0 {
		dst.HeroBannerHeight = src.HeroBannerHeight
	}
	dst.HeroBannerHidden = src.HeroBannerHidden
	if dst.TMDBAPIKey == "" {
		dst.TMDBAPIKey = src.TMDBAPIKey
	}
	if dst.TMDBReadToken == "" {
		dst.TMDBReadToken = src.TMDBReadToken
	}
	dst.RadarrEnabled = src.RadarrEnabled
	if dst.RadarrURL == "" {
		dst.RadarrURL = src.RadarrURL
	}
	if dst.RadarrAPIKey == "" {
		dst.RadarrAPIKey = src.RadarrAPIKey
	}
	if dst.RadarrRootFolder == "" {
		dst.RadarrRootFolder = src.RadarrRootFolder
	}
	if dst.RadarrProfileID == 0 {
		dst.RadarrProfileID = src.RadarrProfileID
	}
	if dst.LGTVAddr == "" {
		dst.LGTVAddr = src.LGTVAddr
	}
	if dst.LGTVClientKey == "" {
		dst.LGTVClientKey = src.LGTVClientKey
	}
	// Snapshot schedule: stored value always wins since zero-value is a valid
	// explicit choice (enabled=true via !Disabled=false, hour=0=midnight).
	dst.SnapshotDisabled = src.SnapshotDisabled
	dst.SnapshotHour = src.SnapshotHour
}
