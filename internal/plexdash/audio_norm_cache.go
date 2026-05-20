package plexdash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const audioCacheDir = "data/audio-profiles"

func audioProfileCacheFile(ratingKey string) string {
	safe := strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(ratingKey)
	return filepath.Join(audioCacheDir, safe+".json")
}

func loadAudioProfile(ratingKey string) (AudioProfile, bool) {
	data, err := os.ReadFile(audioProfileCacheFile(ratingKey))
	if err != nil {
		return AudioProfile{}, false
	}
	var p AudioProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return AudioProfile{}, false
	}
	return p, true
}

func saveAudioProfile(p AudioProfile) error {
	if err := os.MkdirAll(audioCacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(audioProfileCacheFile(p.RatingKey), data, 0o644)
}
