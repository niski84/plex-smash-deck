package plexdash

import (
	"crypto/rand"
	"encoding/hex"
)

// TVDevice represents a TV or streaming device that can receive playback commands.
// The Manufacturer field is used to select the correct control protocol.
// Currently "lg" (LG webOS SSAP) is the only supported manufacturer;
// future values might include "roku", "appletv", "firetv".
type TVDevice struct {
	ID           string `json:"id"`           // stable random hex ID, generated on creation
	Name         string `json:"name"`         // user-friendly display name, e.g. "Living Room"
	Manufacturer string `json:"manufacturer"` // "lg" | "smash-deck" (any service exposing GET /api/smash/manifest + /api/v1/volume/*) | future: "roku", "appletv"
	Addr         string `json:"addr"`         // local IP address
	ClientKey    string `json:"clientKey"`    // SSAP pairing key (LG webOS)
	IPControlKey string `json:"ipControlKey"` // 8-char LG IP Control keycode (optional, for volume)
}

// GenerateDeviceID returns a short random 8-byte hex string suitable for use as a device ID.
func GenerateDeviceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to a fixed-prefix so callers still get a non-empty string.
		return "device-fallback"
	}
	return hex.EncodeToString(b)
}

// FindDeviceByID returns a pointer to the device with the given ID, or nil if not found.
func FindDeviceByID(devices []TVDevice, id string) *TVDevice {
	for i := range devices {
		if devices[i].ID == id {
			return &devices[i]
		}
	}
	return nil
}
