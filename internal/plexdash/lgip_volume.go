package plexdash

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// LG IP Control (TCP :9761) — same wire protocol as lg-webos-smash-deck / lgtv-ip-control.
// Uses TV_KEYCODE / LGTV_IP_CONTROL_KEY, not the SSAP WebSocket client key.

const (
	lgIPPort              = 9761
	lgIPDialTimeout       = 8 * time.Second
	lgIPReadIdle          = 500 * time.Millisecond
	lgIPMsgTerminator     = '\r'
	lgIPRespTerminator    = '\n'
	lgIPMsgBlockSize      = 16
	lgIPEncKeyLen         = 16
	lgIPEncIVLen          = 16
	lgIPEncKeyIterations  = 1 << 14
)

var lgIPEncKeySalt = []byte{
	0x63, 0x61, 0xb8, 0x0e, 0x9b, 0xdc, 0xa6, 0x63,
	0x8d, 0x07, 0x20, 0xf2, 0xcc, 0x56, 0x8f, 0xb9,
}

type lgIPEncoder struct{}

func (lgIPEncoder) encode(msg string) []byte {
	return append([]byte(msg), lgIPMsgTerminator)
}

func (lgIPEncoder) decode(data []byte) string {
	s := string(data)
	if i := strings.IndexByte(s, lgIPRespTerminator); i >= 0 {
		return s[:i]
	}
	return s
}

type lgIPEncryption struct {
	derivedKey []byte
}

func newLgIPEncryptionUpper(keycode string) (*lgIPEncryption, error) {
	keycode = strings.ToUpper(strings.TrimSpace(keycode))
	if len(keycode) != 8 {
		return nil, fmt.Errorf("LG IP control keycode must be 8 characters, got %d", len(keycode))
	}
	key := pbkdf2.Key([]byte(keycode), lgIPEncKeySalt, lgIPEncKeyIterations, lgIPEncKeyLen, sha256.New)
	return &lgIPEncryption{derivedKey: key}, nil
}

func (e *lgIPEncryption) encodeWire(msg string) ([]byte, error) {
	iv := make([]byte, lgIPEncIVLen)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generating IV: %w", err)
	}
	padded := lgIPPadMessage(string(append([]byte(msg), lgIPMsgTerminator)))
	ivEnc, err := lgIPECBEncrypt(e.derivedKey, iv)
	if err != nil {
		return nil, err
	}
	dataEnc, err := lgIPCBCEncrypt(e.derivedKey, iv, []byte(padded))
	if err != nil {
		return nil, err
	}
	return append(ivEnc, dataEnc...), nil
}

func (e *lgIPEncryption) decodeWire(data []byte) (string, error) {
	if len(data) < lgIPEncKeyLen*2 {
		return "", fmt.Errorf("response too short for decryption: %d bytes", len(data))
	}
	ivEnc := data[:lgIPEncKeyLen]
	ciphertext := data[lgIPEncKeyLen:]
	iv, err := lgIPECBDecrypt(e.derivedKey, ivEnc)
	if err != nil {
		return "", err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext length %d not block-aligned", len(ciphertext))
	}
	plaintext, err := lgIPCBCDecrypt(e.derivedKey, iv, ciphertext)
	if err != nil {
		return "", err
	}
	s := string(plaintext)
	if i := strings.IndexByte(s, lgIPRespTerminator); i >= 0 {
		return s[:i], nil
	}
	return s, nil
}

func lgIPPadMessage(msg string) string {
	if len(msg)%lgIPMsgBlockSize == 0 {
		msg = " " + msg
	}
	rem := len(msg) % lgIPMsgBlockSize
	if rem != 0 {
		pad := lgIPMsgBlockSize - rem
		for i := 0; i < pad; i++ {
			msg += string(rune(pad))
		}
	}
	return msg
}

func lgIPECBEncrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ecbEncrypt: data length %d not block-aligned", len(data))
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	return out, nil
}

func lgIPECBDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ecbDecrypt: data length %d not block-aligned", len(data))
	}
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	return out, nil
}

func lgIPCBCEncrypt(key, iv, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
	return out, nil
}

func lgIPCBCDecrypt(key, iv, data []byte) ([]byte, error) {
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("cbcDecrypt: data length %d not block-aligned", len(data))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return out, nil
}

func lgIPReadAll(conn net.Conn, idleTimeout time.Duration) ([]byte, error) {
	var buf bytes.Buffer
	chunk := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		n, err := conn.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if err != nil {
			if err == io.EOF {
				return buf.Bytes(), nil
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			if buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
		if bytes.ContainsRune(buf.Bytes(), lgIPRespTerminator) {
			return buf.Bytes(), nil
		}
	}
}

func lgIPSendCommand(ctx context.Context, host, keycode, command string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("LG TV address is required")
	}
	addr := fmt.Sprintf("%s:%d", host, lgIPPort)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("connect to TV %s: %w", addr, err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(lgIPDialTimeout)
	}
	_ = conn.SetDeadline(deadline)

	var payload []byte
	keycode = strings.TrimSpace(keycode)
	if keycode != "" {
		enc, err := newLgIPEncryptionUpper(keycode)
		if err != nil {
			return "", err
		}
		payload, err = enc.encodeWire(command)
		if err != nil {
			return "", fmt.Errorf("encode command: %w", err)
		}
	} else {
		var enc lgIPEncoder
		payload = enc.encode(command)
	}

	if _, err := conn.Write(payload); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}

	raw, err := lgIPReadAll(conn, lgIPReadIdle)
	if err != nil && len(raw) == 0 {
		return "", fmt.Errorf("read response: %w", err)
	}

	if keycode != "" {
		enc, err := newLgIPEncryptionUpper(keycode)
		if err != nil {
			return "", err
		}
		result, decErr := enc.decodeWire(raw)
		if decErr != nil {
			return "", decErr
		}
		return result, nil
	}
	var enc lgIPEncoder
	return enc.decode(raw), nil
}

var (
	lgIPVolRE  = regexp.MustCompile(`^VOL:(\d+)$`)
	lgIPMuteRE = regexp.MustCompile(`^MUTE:(on|off)$`)
)

// GetVolumeLGIP reads volume and mute via LG IP Control (CURRENT_VOL / MUTE_STATE).
func GetVolumeLGIP(ctx context.Context, host, keycode string) (LGVolumeStatus, error) {
	volStr, err := lgIPSendCommand(ctx, host, keycode, "CURRENT_VOL")
	if err != nil {
		return LGVolumeStatus{}, err
	}
	m := lgIPVolRE.FindStringSubmatch(strings.TrimSpace(volStr))
	if m == nil {
		return LGVolumeStatus{}, fmt.Errorf("parse CURRENT_VOL: %q", volStr)
	}
	vol, err := strconv.Atoi(m[1])
	if err != nil {
		return LGVolumeStatus{}, fmt.Errorf("parse CURRENT_VOL volume: %w", err)
	}
	muteStr, err := lgIPSendCommand(ctx, host, keycode, "MUTE_STATE")
	if err != nil {
		return LGVolumeStatus{Volume: vol}, nil
	}
	mm := lgIPMuteRE.FindStringSubmatch(strings.TrimSpace(muteStr))
	muted := false
	if mm != nil {
		muted = mm[1] == "on"
	}
	return LGVolumeStatus{Volume: vol, Mute: muted}, nil
}

// SetVolumeLGIP sets absolute volume via VOLUME_CONTROL (same as lg-webos-smash-deck).
func SetVolumeLGIP(ctx context.Context, host, keycode string, level int) (LGVolumeStatus, error) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	resp, err := lgIPSendCommand(ctx, host, keycode, fmt.Sprintf("VOLUME_CONTROL %d", level))
	if err != nil {
		return LGVolumeStatus{}, err
	}
	if strings.TrimSpace(resp) != "OK" {
		return LGVolumeStatus{}, fmt.Errorf("VOLUME_CONTROL: %q", resp)
	}
	return GetVolumeLGIP(ctx, host, keycode)
}
