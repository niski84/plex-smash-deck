package plexdash

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AudioProfile is the result of an FFmpeg volumedetect pass on a movie file.
type AudioProfile struct {
	RatingKey    string    `json:"ratingKey"`
	Title        string    `json:"title"`
	MeanVolumeDB float64   `json:"meanVolumeDB"` // e.g. -28.5
	MaxVolumeDB  float64   `json:"maxVolumeDB"`  // e.g. -4.2
	AnalyzedAt   time.Time `json:"analyzedAt"`
}

// NormResult is the computed recommendation from an AudioProfile.
type NormResult struct {
	Profile            AudioProfile `json:"profile"`
	RecommendedVolume  int          `json:"recommendedVolume"`  // 0–100 TV volume
	NormalizedLevel    int          `json:"normalizedLevel"`    // 1–10 convenience scale
	DeltaDB            float64      `json:"deltaDB"`            // positive = movie is quiet
	BaselineVolume     int          `json:"baselineVolume"`
	ReferenceDB        float64      `json:"referenceDB"`
}

// NormalizeVolume converts an AudioProfile into a TV volume recommendation.
//
// Formula:
//   delta = referenceDB - meanVolumeDB   (positive → movie is quiet → raise volume)
//   Each 2 dB maps to 1 TV volume unit (scale factor 0.5).
//   Result is clamped to [max(1, baseline-25), min(100, baseline+25)].
//   normalizedLevel 1–10 maps the same range linearly.
func NormalizeVolume(p AudioProfile, baselineVolume int, referenceDB float64) NormResult {
	if baselineVolume <= 0 {
		baselineVolume = 50
	}
	if referenceDB == 0 {
		referenceDB = -23.0
	}

	deltaDB := referenceDB - p.MeanVolumeDB
	// 2 dB per TV volume unit — adjust aggressiveness here if needed
	rawAdj := deltaDB * 0.5
	recommended := baselineVolume + int(math.Round(rawAdj))

	lo := max(1, baselineVolume-25)
	hi := min(100, baselineVolume+25)
	if recommended < lo {
		recommended = lo
	}
	if recommended > hi {
		recommended = hi
	}

	// Map recommended volume to 1–10 scale centred on baseline
	span := float64(hi - lo)
	level := 1
	if span > 0 {
		level = int(math.Round(float64(recommended-lo)/span*9)) + 1
	}
	if level < 1 {
		level = 1
	}
	if level > 10 {
		level = 10
	}

	return NormResult{
		Profile:           p,
		RecommendedVolume: recommended,
		NormalizedLevel:   level,
		DeltaDB:           deltaDB,
		BaselineVolume:    baselineVolume,
		ReferenceDB:       referenceDB,
	}
}

var (
	reMeanVol = regexp.MustCompile(`mean_volume:\s*([-\d.]+)\s*dB`)
	reMaxVol  = regexp.MustCompile(`max_volume:\s*([-\d.]+)\s*dB`)
)

// AnalyzeAudioFile runs FFmpeg's volumedetect filter on a 2-minute sample of
// the file starting at the 5-minute mark (skips opening credits/silence).
// It returns an AudioProfile with mean and max volume in dBFS.
//
// filePath must be the actual filesystem path to the media file.
// ctx should carry a deadline; the function adds its own 90s timeout on top.
func AnalyzeAudioFile(ctx context.Context, ratingKey, title, filePath string) (AudioProfile, error) {
	if strings.TrimSpace(filePath) == "" {
		return AudioProfile{}, fmt.Errorf("file path is empty for %q", title)
	}

	tctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// -ss before -i enables fast seek; -t 120 = 2 minutes.
	// -vn -sn -dn = skip video/subtitles/data — audio only.
	// -af volumedetect -f null /dev/null triggers the filter without writing output.
	args := []string{
		"-hide_banner",
		"-nostats",
		"-ss", "00:05:00",
		"-i", filePath,
		"-t", "00:02:00",
		"-vn", "-sn", "-dn",
		"-af", "volumedetect",
		"-f", "null",
		"/dev/null",
	}
	cmd := exec.CommandContext(tctx, "ffmpeg", args...)

	// FFmpeg writes volumedetect output to stderr.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// volumedetect output is still in stderr even on non-zero exit for some codecs.
		if !bytes.Contains(stderr.Bytes(), []byte("mean_volume")) {
			return AudioProfile{}, fmt.Errorf("ffmpeg failed: %w — %s", err, trimFFmpegStderr(stderr.String()))
		}
	}

	mean, max, err := parseVolumedetect(stderr.String())
	if err != nil {
		return AudioProfile{}, fmt.Errorf("parse ffmpeg output: %w", err)
	}

	return AudioProfile{
		RatingKey:    ratingKey,
		Title:        title,
		MeanVolumeDB: mean,
		MaxVolumeDB:  max,
		AnalyzedAt:   time.Now().UTC(),
	}, nil
}

func parseVolumedetect(output string) (mean, maxVol float64, err error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var meanStr, maxStr string
	for scanner.Scan() {
		line := scanner.Text()
		if m := reMeanVol.FindStringSubmatch(line); len(m) == 2 {
			meanStr = m[1]
		}
		if m := reMaxVol.FindStringSubmatch(line); len(m) == 2 {
			maxStr = m[1]
		}
	}
	if meanStr == "" {
		return 0, 0, fmt.Errorf("mean_volume not found in ffmpeg output")
	}
	mean, err = strconv.ParseFloat(meanStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse mean_volume %q: %w", meanStr, err)
	}
	if maxStr != "" {
		maxVol, _ = strconv.ParseFloat(maxStr, 64)
	}
	return mean, maxVol, nil
}

// trimFFmpegStderr returns the last few lines of FFmpeg stderr for error messages.
func trimFFmpegStderr(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.Join(lines, " | ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
