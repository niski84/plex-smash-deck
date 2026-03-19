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
	Port             string
	PlexBaseURL      string
	PlexToken        string
	LibraryKey       string
	TargetClientName string
	TMDBAPIKey       string
	TMDBReadToken    string
	RadarrURL        string
	RadarrAPIKey     string
	RadarrRootFolder string
	RadarrProfileID  int
}

func LoadConfig() (Config, error) {
	_ = loadDotEnv(".env")

	cfg := Config{
		Port:             getenv("PORT", "8081"),
		PlexBaseURL:      strings.TrimRight(os.Getenv("PLEX_BASE_URL"), "/"),
		PlexToken:        os.Getenv("PLEX_TOKEN"),
		LibraryKey:       getenv("PLEX_LIBRARY_KEY", "1"),
		TargetClientName: getenv("PLEX_TARGET_CLIENT_NAME", "Living Room"),
		TMDBAPIKey:       os.Getenv("TMDB_API_KEY"),
		TMDBReadToken:    os.Getenv("TMDB_READ_ACCESS_TOKEN"),
		RadarrURL:        strings.TrimRight(os.Getenv("RADARR_URL"), "/"),
		RadarrAPIKey:     os.Getenv("RADARR_API_KEY"),
		RadarrRootFolder: os.Getenv("RADARR_ROOT_FOLDER"),
		RadarrProfileID:  getenvInt("RADARR_PROFILE_ID", 1),
	}

	// Source from .env first, then fill missing values from persisted settings.
	stored, err := loadPersistedConfig(defaultSettingsPath())
	if err == nil {
		mergeMissingConfig(&cfg, stored)
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
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, strings.Trim(val, `"'`))
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
	if dst.TMDBAPIKey == "" {
		dst.TMDBAPIKey = src.TMDBAPIKey
	}
	if dst.TMDBReadToken == "" {
		dst.TMDBReadToken = src.TMDBReadToken
	}
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
}
