package plexdash

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// LGVolumeStatus is the TV audio state returned by SSAP helpers used by /api/lg/volume.
type LGVolumeStatus struct {
	Volume int
	Mute   bool
}

// GetVolumeSSAP reads current TV volume and mute via ssap://audio/getStatus.
func GetVolumeSSAP(ctx context.Context, tvIP, clientKey string) (LGVolumeStatus, error) {
	v, m, err := LGSSAPVolumeStatus(ctx, tvIP, clientKey)
	if err != nil {
		return LGVolumeStatus{}, err
	}
	return LGVolumeStatus{Volume: v, Mute: m}, nil
}

// SetVolumeSSAP sets volume via ssap://audio/setVolume and re-reads status so mute/volume match the TV.
func SetVolumeSSAP(ctx context.Context, tvIP, clientKey string, level int) (LGVolumeStatus, error) {
	if _, err := LGSSAPSetVolume(ctx, tvIP, clientKey, level); err != nil {
		return LGVolumeStatus{}, err
	}
	return GetVolumeSSAP(ctx, tvIP, clientKey)
}

// LGSSAPVolumeStatus returns current TV volume (0–100) and mute from ssap://audio/getStatus.
// Uses extended SSAP manifest permissions (CONTROL_AUDIO); pairing must allow read APIs.
func LGSSAPVolumeStatus(ctx context.Context, tvIP, clientKey string) (volume int, mute bool, err error) {
	tvIP = strings.TrimSpace(tvIP)
	clientKey = strings.TrimSpace(clientKey)
	if tvIP == "" || clientKey == "" {
		return 0, false, fmt.Errorf("LG TV address and client key are required")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, err := dialSSAP(dialCtx, tvIP)
	if err != nil {
		return 0, false, fmt.Errorf("LG SSAP dial: %w", err)
	}
	defer conn.Close()
	conn.conn.SetDeadline(time.Now().Add(25 * time.Second)) //nolint:errcheck
	if err := ssapRegisterManifest(conn, clientKey, lgWebOSRemoteStylePermissions); err != nil {
		return 0, false, fmt.Errorf("LG SSAP register: %w", err)
	}
	if err := ssapSendRequest(conn, "aud0", "ssap://audio/getStatus", nil); err != nil {
		return 0, false, err
	}
	resp, err := recvSSAPResponseForID(conn, "aud0", 24)
	if err != nil {
		return 0, false, err
	}
	return parseAudioGetStatus(resp)
}

// LGSSAPSetVolume sets TV volume via ssap://audio/setVolume (0–100).
func LGSSAPSetVolume(ctx context.Context, tvIP, clientKey string, level int) (confirmed int, err error) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	tvIP = strings.TrimSpace(tvIP)
	clientKey = strings.TrimSpace(clientKey)
	if tvIP == "" || clientKey == "" {
		return 0, fmt.Errorf("LG TV address and client key are required")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, err := dialSSAP(dialCtx, tvIP)
	if err != nil {
		return 0, fmt.Errorf("LG SSAP dial: %w", err)
	}
	defer conn.Close()
	conn.conn.SetDeadline(time.Now().Add(25 * time.Second)) //nolint:errcheck
	if err := ssapRegisterManifest(conn, clientKey, lgWebOSRemoteStylePermissions); err != nil {
		return 0, fmt.Errorf("LG SSAP register: %w", err)
	}
	payload := map[string]any{"volume": level}
	if err := ssapSendRequest(conn, "aud1", "ssap://audio/setVolume", payload); err != nil {
		return 0, err
	}
	resp, err := recvSSAPResponseForID(conn, "aud1", 24)
	if err != nil {
		return 0, err
	}
	vol, _, err := parseAudioGetStatus(resp)
	if err != nil {
		// Some firmwares return a minimal payload; re-read status.
		return level, nil
	}
	return vol, nil
}

func parseAudioGetStatus(resp map[string]any) (volume int, mute bool, err error) {
	if typ, _ := resp["type"].(string); typ == "error" {
		return 0, false, fmt.Errorf("ssap error: %v", resp["error"])
	}
	payload, _ := resp["payload"].(map[string]any)
	if payload == nil {
		return 0, false, fmt.Errorf("no payload in audio response")
	}
	if rv, ok := payload["returnValue"].(bool); ok && !rv {
		return 0, false, fmt.Errorf("audio request failed: %v", payload)
	}
	mute, _ = payload["mute"].(bool)
	volume = intFromAny(payload["volume"])
	if volume == 0 {
		if vs, ok := payload["volumeStatus"].(map[string]any); ok {
			volume = intFromAny(vs["volume"])
		}
	}
	return volume, mute, nil
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case float64:
		return int(math.Round(x))
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
	}
}
