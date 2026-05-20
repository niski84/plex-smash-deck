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

// SetVolumeSSAP sets volume by stepping with ssap://audio/volumeUp|volumeDown,
// which emulates remote-button presses. This is CEC/SIMPLINK-forwarded to
// HDMI ARC soundbars and AV receivers (the same path the LG remote and ThinQ
// app use). ssap://audio/setVolume writes directly to the TV's internal
// speaker and silently does nothing on ARC-routed setups, so we avoid it.
func SetVolumeSSAP(ctx context.Context, tvIP, clientKey string, level int) (LGVolumeStatus, error) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	tvIP = strings.TrimSpace(tvIP)
	clientKey = strings.TrimSpace(clientKey)
	if tvIP == "" || clientKey == "" {
		return LGVolumeStatus{}, fmt.Errorf("LG TV address and client key are required")
	}

	const (
		maxRounds    = 4
		maxStepsPerRound = 100
		stepInterval = 40 * time.Millisecond
	)

	for round := 0; round < maxRounds; round++ {
		select {
		case <-ctx.Done():
			return LGVolumeStatus{}, ctx.Err()
		default:
		}

		st, err := stepVolumeSSAPOnce(ctx, tvIP, clientKey, level, maxStepsPerRound, stepInterval)
		if err != nil {
			return LGVolumeStatus{}, err
		}
		if st.Volume == level {
			return st, nil
		}
	}
	return GetVolumeSSAP(ctx, tvIP, clientKey)
}

// stepVolumeSSAPOnce opens a single SSAP session, reads the current volume,
// and steps it toward target using audio/volumeUp|volumeDown. Returns the
// post-step status.
func stepVolumeSSAPOnce(ctx context.Context, tvIP, clientKey string, target, maxSteps int, interval time.Duration) (LGVolumeStatus, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, err := dialSSAP(dialCtx, tvIP)
	if err != nil {
		return LGVolumeStatus{}, fmt.Errorf("LG SSAP dial: %w", err)
	}
	defer conn.Close()
	conn.conn.SetDeadline(time.Now().Add(45 * time.Second)) //nolint:errcheck
	if err := ssapRegisterManifest(conn, clientKey, lgWebOSRemoteStylePermissions); err != nil {
		return LGVolumeStatus{}, fmt.Errorf("LG SSAP register: %w", err)
	}

	if err := ssapSendRequest(conn, "aud0", "ssap://audio/getStatus", nil); err != nil {
		return LGVolumeStatus{}, err
	}
	resp, err := recvSSAPResponseForID(conn, "aud0", 24)
	if err != nil {
		return LGVolumeStatus{}, err
	}
	vol, mute, err := parseAudioGetStatus(resp)
	if err != nil {
		return LGVolumeStatus{}, err
	}

	delta := target - vol
	if delta == 0 {
		return LGVolumeStatus{Volume: vol, Mute: mute}, nil
	}
	uri := "ssap://audio/volumeUp"
	steps := delta
	if delta < 0 {
		uri = "ssap://audio/volumeDown"
		steps = -delta
	}
	if steps > maxSteps {
		steps = maxSteps
	}

	for i := 0; i < steps; i++ {
		select {
		case <-ctx.Done():
			return LGVolumeStatus{}, ctx.Err()
		default:
		}
		id := fmt.Sprintf("vstep%d", i)
		if err := ssapSendRequest(conn, id, uri, nil); err != nil {
			return LGVolumeStatus{}, err
		}
		if _, err := recvSSAPResponseForID(conn, id, 8); err != nil {
			return LGVolumeStatus{}, err
		}
		if interval > 0 && i+1 < steps {
			time.Sleep(interval)
		}
	}

	if err := ssapSendRequest(conn, "aud9", "ssap://audio/getStatus", nil); err != nil {
		return LGVolumeStatus{}, err
	}
	resp2, err := recvSSAPResponseForID(conn, "aud9", 16)
	if err != nil {
		return LGVolumeStatus{}, err
	}
	vol2, mute2, err := parseAudioGetStatus(resp2)
	if err != nil {
		return LGVolumeStatus{Volume: vol, Mute: mute}, nil
	}
	return LGVolumeStatus{Volume: vol2, Mute: mute2}, nil
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
