package plexdash

// lgssap.go — minimal LG webOS SSAP WebSocket client
//
// LG Smart TVs expose a WebSocket API on port 3001 (wss://) called SSAP.
// Through SSAP we authenticate once (pairing prompt on TV), store the client
// key, and then control the TV.
//
// Playlist playback uses the webOS native media player (com.webos.app.mediadiscovery)
// with direct Plex HTTP stream URLs, bypassing the Plex companion protocol entirely.
// The webOS app accepts an array of items in its launch params and plays them in sequence.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	ssapPort         = "3001"
	ssapOrigin       = "null"
	plexLGAppID      = "cdp-30"
	webOSMediaAppID  = "com.webos.app.mediadiscovery"
)

// ssapConn is a minimal WSS (WebSocket over TLS) connection to an LG TV.
type ssapConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// dialSSAP opens a WSS connection to an LG webOS TV at tvIP on port 3001.
// The TV uses a self-signed certificate so TLS verification is skipped.
func dialSSAP(ctx context.Context, tvIP string) (*ssapConn, error) {
	addr := tvIP + ":" + ssapPort
	dialer := &tls.Dialer{
		Config: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec — LG TV self-signed cert
	}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	// Generate a random WebSocket handshake key.
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		raw.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	upgrade := "GET / HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: " + ssapOrigin + "\r\n\r\n"
	if _, err := raw.Write([]byte(upgrade)); err != nil {
		raw.Close()
		return nil, fmt.Errorf("write upgrade: %w", err)
	}

	br := bufio.NewReader(raw)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("read upgrade response: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw.Close()
		return nil, fmt.Errorf("unexpected HTTP status %d (expected 101)", resp.StatusCode)
	}

	// Verify the server's accept key.
	want := wsAcceptKey(key)
	if got := resp.Header.Get("Sec-Websocket-Accept"); !strings.EqualFold(got, want) {
		raw.Close()
		return nil, fmt.Errorf("bad Sec-WebSocket-Accept: got %q want %q", got, want)
	}

	return &ssapConn{conn: raw, br: br}, nil
}

func wsAcceptKey(key string) string {
	const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.Sum([]byte(key + magic)) //nolint:gosec — WebSocket spec mandated
	return base64.StdEncoding.EncodeToString(h[:])
}

func (c *ssapConn) Close() { c.conn.Close() }

// sendJSON marshals v and sends it as a masked WebSocket text frame.
func (c *ssapConn) sendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x81, data) // 0x81 = FIN + text opcode
}

// writeFrame builds a client-to-server WebSocket frame with masking.
func (c *ssapConn) writeFrame(header byte, payload []byte) error {
	var maskKey [4]byte
	if _, err := rand.Read(maskKey[:]); err != nil {
		return err
	}
	plen := len(payload)

	var buf []byte
	switch {
	case plen < 126:
		buf = append(buf, header, 0x80|byte(plen))
	case plen < 65536:
		buf = append(buf, header, 0x80|126)
		buf = append(buf, byte(plen>>8), byte(plen))
	default:
		buf = append(buf, header, 0x80|127)
		var lenBytes [8]byte
		binary.BigEndian.PutUint64(lenBytes[:], uint64(plen))
		buf = append(buf, lenBytes[:]...)
	}
	buf = append(buf, maskKey[:]...)
	for i, b := range payload {
		buf = append(buf, b^maskKey[i%4])
	}
	_, err := c.conn.Write(buf)
	return err
}

// recvJSON reads one WebSocket text frame and unmarshals JSON into v.
func (c *ssapConn) recvJSON(v any) error {
	payload, err := c.readFrame()
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, v)
}

// readFrame reads one server-to-client WebSocket frame.
// It transparently handles ping frames and returns data frames.
func (c *ssapConn) readFrame() ([]byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return nil, err
	}
	opcode := h[0] & 0x0F
	masked := h[1]&0x80 != 0
	plen := int(h[1] & 0x7F)

	switch plen {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return nil, err
		}
		plen = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return nil, err
		}
		plen = int(binary.BigEndian.Uint64(ext[:]))
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, plen)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return nil, err
	}
	if masked {
		for i, b := range payload {
			payload[i] = b ^ maskKey[i%4]
		}
	}

	switch opcode {
	case 0x9: // ping — respond with pong
		_ = c.writeFrame(0x8A, payload)
		return c.readFrame()
	case 0x8: // close
		return nil, fmt.Errorf("websocket connection closed by server")
	}
	return payload, nil
}

// ssapMsg is the generic SSAP envelope.
type ssapMsg struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	URI     string `json:"uri,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// ssapRegister authenticates with the TV using a stored client key.
// If the key is empty the TV will show a pairing prompt; that path is only
// needed during initial setup (see PairLGTV).
func ssapRegister(conn *ssapConn, clientKey string) error {
	msg := ssapMsg{
		ID:   "reg0",
		Type: "register",
		Payload: map[string]any{
			"forcePairing": false,
			"pairingType":  "PROMPT",
			"client-key":   clientKey,
			"manifest": map[string]any{
				"appVersion":      "1.1",
				"manifestVersion": 1,
				"permissions": []string{
					"LAUNCH",
					"CONTROL_INPUT_MEDIA_PLAYBACK",
					"CONTROL_INPUT_JOYSTICK",
					"TEST_OPEN",
					"TEST_PROTECTED",
				},
			},
		},
	}
	if err := conn.sendJSON(msg); err != nil {
		return err
	}
	var resp map[string]any
	if err := conn.recvJSON(&resp); err != nil {
		return err
	}
	if t, _ := resp["type"].(string); t != "registered" {
		return fmt.Errorf("SSAP registration failed (type=%q payload=%v)", t, resp["payload"])
	}
	return nil
}

// WebOSStreamItem describes one video item for the webOS native media player.
type WebOSStreamItem struct {
	StreamURL string
	Title     string
	Container string // "mp4", "mkv", "avi", etc.
	Size      int64
}

// PlayPlaylistViaWebOS streams a playlist of Plex items through the LG webOS
// native media player (com.webos.app.mediadiscovery). It bypasses the Plex
// companion protocol entirely — the TV receives direct HTTP stream URLs and
// plays them in sequence. Works reliably on webOS 6+ (e.g. webOS 10.2.x).
func PlayPlaylistViaWebOS(ctx context.Context, tvIP, clientKey string, items []WebOSStreamItem) error {
	if len(items) == 0 {
		return fmt.Errorf("playlist is empty")
	}

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, err := dialSSAP(dialCtx, tvIP)
	if err != nil {
		return fmt.Errorf("LG SSAP dial: %w", err)
	}
	defer conn.Close()

	conn.conn.SetDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck
	if err := ssapRegister(conn, clientKey); err != nil {
		return fmt.Errorf("LG SSAP register: %w", err)
	}

	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mimeType := "video/" + item.Container
		if item.Container == "mkv" {
			mimeType = "video/x-matroska"
		}
		sizeStr := "-1"
		if item.Size > 0 {
			sizeStr = fmt.Sprintf("%d", item.Size)
		}
		payload = append(payload, map[string]any{
			"fullPath":        item.StreamURL,
			"mediaType":       "VIDEO",
			"fileName":        item.Title,
			"thumbnail":       "",
			"subtitle":        "",
			"lastPlayPosition": -1,
			"deviceType":      "DMR",
			"dlnaInfo": map[string]any{
				"flagVal":       4096,
				"cleartextSize": sizeStr,
				"contentLength": sizeStr,
				"opVal":         1,
				"protocolInfo":  fmt.Sprintf("http-get:*:%s:DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000", mimeType),
				"duration":      0,
			},
		})
	}

	launch := ssapMsg{
		ID:   "launch1",
		Type: "request",
		URI:  "ssap://system.launcher/launch",
		Payload: map[string]any{
			"id": webOSMediaAppID,
			"params": map[string]any{
				"payload": payload,
			},
		},
	}
	if err := conn.sendJSON(launch); err != nil {
		return fmt.Errorf("LG SSAP launch: %w", err)
	}
	var launchResp map[string]any
	if err := conn.recvJSON(&launchResp); err != nil {
		return fmt.Errorf("LG SSAP launch response: %w", err)
	}
	respPayload, _ := launchResp["payload"].(map[string]any)
	if ok, _ := respPayload["returnValue"].(bool); !ok {
		return fmt.Errorf("LG SSAP launch failed: %v", respPayload)
	}

	fmt.Printf("[lgssap] webOS media player launched with %d items on %s\n", len(items), tvIP)
	return nil
}

// PairLGTV performs the one-time SSAP pairing handshake. The TV will display
// an on-screen confirmation prompt — the user must accept it. Returns the
// client key to store in config for future use.
func PairLGTV(ctx context.Context, tvIP string) (clientKey string, err error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := dialSSAP(dialCtx, tvIP)
	if err != nil {
		return "", fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	msg := ssapMsg{
		ID:   "reg0",
		Type: "register",
		Payload: map[string]any{
			"forcePairing": false,
			"pairingType":  "PROMPT",
			"client-key":   "",
			"manifest": map[string]any{
				"appVersion":      "1.1",
				"manifestVersion": 1,
				"permissions": []string{
					"LAUNCH",
					"CONTROL_INPUT_MEDIA_PLAYBACK",
					"CONTROL_INPUT_JOYSTICK",
					"TEST_OPEN",
					"TEST_PROTECTED",
				},
			},
		},
	}
	if err := conn.sendJSON(msg); err != nil {
		return "", err
	}

	// First response is the pairing prompt confirmation (pairingType=PROMPT).
	// Second response (after user accepts on TV) carries the client-key.
	conn.conn.SetDeadline(time.Now().Add(45 * time.Second)) //nolint:errcheck
	for range 2 {
		var resp map[string]any
		if err := conn.recvJSON(&resp); err != nil {
			return "", fmt.Errorf("receive pairing response: %w", err)
		}
		payload, _ := resp["payload"].(map[string]any)
		if k, ok := payload["client-key"].(string); ok && k != "" {
			return k, nil
		}
	}
	return "", fmt.Errorf("no client-key received (did you accept on the TV?)")
}
