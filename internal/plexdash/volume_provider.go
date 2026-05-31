package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SmashDeckVolumeStatus is the normalized volume state from any smash-deck service.
type SmashDeckVolumeStatus struct {
	Volume int
	Muted  bool
}

// GetVolumeSmashDeck fetches current volume from a smash-deck compatible service.
// addr may be "host:port" or "http://host:port".
func GetVolumeSmashDeck(ctx context.Context, addr string) (SmashDeckVolumeStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, smashDeckBase(addr)+"/api/state", nil)
	if err != nil {
		return SmashDeckVolumeStatus{}, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return SmashDeckVolumeStatus{}, err
	}
	defer resp.Body.Close()
	var st struct {
		Volume int  `json:"volume"`
		Muted  bool `json:"muted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return SmashDeckVolumeStatus{}, err
	}
	return SmashDeckVolumeStatus{Volume: st.Volume, Muted: st.Muted}, nil
}

// SetVolumeSmashDeck sends a volume level (0-100) to a smash-deck compatible service.
func SetVolumeSmashDeck(ctx context.Context, addr string, level int) (SmashDeckVolumeStatus, error) {
	url := fmt.Sprintf("%s/api/v1/volume/set?level=%d", smashDeckBase(addr), level)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return SmashDeckVolumeStatus{}, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return SmashDeckVolumeStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return SmashDeckVolumeStatus{}, fmt.Errorf("smash-deck volume: HTTP %d", resp.StatusCode)
	}
	var st struct {
		Volume int  `json:"volume"`
		Muted  bool `json:"muted"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&st)
	return SmashDeckVolumeStatus{Volume: st.Volume, Muted: st.Muted}, nil
}

func smashDeckBase(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + strings.TrimRight(addr, "/")
}
