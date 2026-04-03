package plexdash

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	rokuECPPort       = 8060
	rokuPlexChannelID = "13535"
	rokuECPTimeout    = 10 * time.Second
)

// rokuECPBase returns the base URL for ECP commands on the given Roku address.
func rokuECPBase(addr string) string {
	host := strings.TrimSpace(addr)
	if !strings.Contains(host, ":") {
		host = fmt.Sprintf("%s:%d", host, rokuECPPort)
	}
	return "http://" + host
}

// rokuPost sends a POST request to the Roku ECP endpoint with no body.
func rokuPost(ctx context.Context, addr, path string) error {
	ctx, cancel := context.WithTimeout(ctx, rokuECPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rokuECPBase(addr)+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("roku ECP POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("roku ECP POST %s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// RokuDeviceInfo holds basic info returned by ECP /query/device-info.
type RokuDeviceInfo struct {
	FriendlyName    string `xml:"friendly-device-name"`
	ModelName       string `xml:"model-name"`
	SoftwareVersion string `xml:"software-version"`
}

// QueryRokuDeviceInfo fetches device info from the Roku ECP endpoint.
func QueryRokuDeviceInfo(ctx context.Context, addr string) (RokuDeviceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, rokuECPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rokuECPBase(addr)+"/query/device-info", nil)
	if err != nil {
		return RokuDeviceInfo{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return RokuDeviceInfo{}, fmt.Errorf("roku ECP query: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return RokuDeviceInfo{}, err
	}
	var info RokuDeviceInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return RokuDeviceInfo{}, fmt.Errorf("roku ECP device-info decode: %w", err)
	}
	return info, nil
}

// PlayOnRokuViaPlex launches the Plex channel on the Roku and deep-links to the
// first item in items. Roku ECP does not support native queuing, so only the
// first title is launched directly; additional titles are ignored (user can
// navigate within Plex on the TV for multi-item play).
//
// The plexMachineID is the Plex Media Server machine identifier (used to
// construct the deep-link content ID). If blank, only the channel is launched
// without a content deep link.
func PlayOnRokuViaPlex(ctx context.Context, addr, plexMachineID string, items []WebOSStreamItem, ratingKeys []string) error {
	if len(items) == 0 {
		return fmt.Errorf("no items to play")
	}

	params := url.Values{}

	if plexMachineID != "" && len(ratingKeys) > 0 {
		rk := ratingKeys[0]
		// Plex Roku channel deep-link format:
		//   contentId={machineId}/library/metadata/{ratingKey}&mediaType=movie
		params.Set("contentId", plexMachineID+"/library/metadata/"+rk)
		params.Set("mediaType", "movie")
	}

	path := "/launch/" + rokuPlexChannelID
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	fmt.Printf("[roku] launching Plex channel on %s: %s\n", addr, path)
	return rokuPost(ctx, addr, path)
}

// RokuKeypress sends a single keypress to the Roku via ECP.
// Common keys: VolumeUp, VolumeDown, VolumeMute, Home, Play, Pause, Select.
func RokuKeypress(ctx context.Context, addr, key string) error {
	return rokuPost(ctx, addr, "/keypress/"+url.PathEscape(key))
}
