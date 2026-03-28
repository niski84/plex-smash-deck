package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SSAPProbeResult is one experimental SSAP request and the TV's reply (or error).
type SSAPProbeResult struct {
	Name string
	URI  string
	OK   bool
	Err  string
	// Response is the raw decoded JSON object (type response / error), when OK or parseable.
	Response map[string]any
}

func (r SSAPProbeResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n  uri: %s\n", r.Name, r.URI)
	if r.Err != "" {
		fmt.Fprintf(&b, "  error: %s\n", r.Err)
		return b.String()
	}
	if r.Response != nil {
		fmt.Fprintf(&b, "  ok: %v\n", r.OK)
		raw, _ := json.MarshalIndent(r.Response, "  ", "  ")
		fmt.Fprintf(&b, "  %s\n", string(raw))
	}
	return b.String()
}

// ssapProbeQueries is a curated list of SSAP URIs used by community tools (e.g. LGWebOSRemote)
// to probe foreground app, audio, power, and system services. Titles/metadata for arbitrary
// playback are not guaranteed.
var ssapProbeQueries = []struct {
	Name    string
	URI     string
	Payload any
}{
	{"foreground_app (applicationManager)", "ssap://com.webos.applicationManager/getForegroundAppInfo", nil},
	{"foreground_app (service.applicationmanager)", "ssap://com.webos.service.applicationmanager/getForegroundAppInfo", nil},
	{"com.webos.media getForegroundAppInfo", "ssap://com.webos.media/getForegroundAppInfo", nil},
	{"audio getStatus", "ssap://audio/getStatus", nil},
	{"audio getVolume", "ssap://audio/getVolume", nil},
	{"power getPowerState", "ssap://com.webos.service.tvpower/power/getPowerState", nil},
	{"api getServiceList", "ssap://api/getServiceList", nil},
	{"update getCurrentSWInformation", "ssap://com.webos.service.update/getCurrentSWInformation", nil},
	{"tv getCurrentChannel", "ssap://tv/getCurrentChannel", nil},
	{"system getSystemInfo", "ssap://system/getSystemInfo", nil},
	{"listApps", "ssap://com.webos.applicationManager/listApps", nil},
}

// SSAPProbeExperiments dials the TV, registers with the stored client key, and runs each
// probe on a fresh connection so responses cannot get mixed. Use this to see which Luna/SSAP
// surfaces return data on your firmware.
func SSAPProbeExperiments(ctx context.Context, tvIP, clientKey string) []SSAPProbeResult {
	if strings.TrimSpace(tvIP) == "" || strings.TrimSpace(clientKey) == "" {
		return []SSAPProbeResult{{
			Name: "config",
			OK:   false,
			Err:  "LGTV_ADDR and LGTV_CLIENT_KEY are required",
		}}
	}

	out := make([]SSAPProbeResult, 0, len(ssapProbeQueries)+1)
	for _, q := range ssapProbeQueries {
		r := SSAPProbeResult{Name: q.Name, URI: q.URI}
		dialCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		conn, err := dialSSAP(dialCtx, tvIP)
		cancel()
		if err != nil {
			r.Err = fmt.Sprintf("dial: %v", err)
			out = append(out, r)
			continue
		}
		func() {
			defer conn.Close()
			conn.conn.SetDeadline(time.Now().Add(25 * time.Second)) //nolint:errcheck
			if err := ssapRegisterManifest(conn, clientKey, lgWebOSRemoteStylePermissions); err != nil {
				r.Err = fmt.Sprintf("register: %v", err)
				return
			}
			if err := ssapSendRequest(conn, "probe", q.URI, q.Payload); err != nil {
				r.Err = fmt.Sprintf("send: %v", err)
				return
			}
			resp, err := recvSSAPResponse(conn, 8)
			if err != nil {
				r.Err = fmt.Sprintf("recv: %v", err)
				return
			}
			r.Response = resp
			if typ, _ := resp["type"].(string); typ == "response" {
				r.OK = true
			}
		}()
		out = append(out, r)
	}
	return out
}
