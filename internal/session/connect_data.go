// Package session holds the transport-agnostic pieces shared by the TUI and
// the WebRTC engine: the room identity and the serialized "connect data"
// blob that another client can paste in order to join the same call.
package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Magic prefixes every connect-data blob so we can recognise our own format.
var magic = [4]byte{'W', 'R', 'M', 0x01}

// Version of the connect-data wire format. Bump on breaking changes.
const Version byte = 1

// maxFieldLen guards against absurd/hostile payloads.
const maxFieldLen = 512

// ConnectData is the minimal set of facts a second client needs in order to
// reach the room offered by the first one.
type ConnectData struct {
	// Room is the human-readable room name.
	Room string

	// Name is the display name of the peer that produced this blob.
	Name string

	// Fingerprint is the DTLS certificate fingerprint of the offering peer.
	// It pins the identity of the remote side; empty when not yet known.
	Fingerprint string
}

// ErrInvalid is returned when a blob cannot be decoded.
var ErrInvalid = errors.New("invalid connect data")

// Encode serializes the connect data into lowercase hex.
//
// Layout: MAGIC(4) | VERSION(1) | len(room) u16 | room | len(name) u16 | name
// | len(fingerprint) u16 | fingerprint
//
// The whole payload is hex-encoded, so the result is safe to paste anywhere.
func Encode(d ConnectData) (string, error) {
	if err := validate(d); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	buf.Write(magic[:])
	buf.WriteByte(Version)

	for _, field := range []string{d.Room, d.Name, d.Fingerprint} {
		if len(field) > maxFieldLen {
			return "", fmt.Errorf("%w: field too long (%d bytes)", ErrInvalid, len(field))
		}
		var n [2]byte
		n[0] = byte(len(field) >> 8)
		n[1] = byte(len(field))
		buf.Write(n[:])
		buf.WriteString(field)
	}

	return hex.EncodeToString(buf.Bytes()), nil
}

// Decode parses a hex blob produced by Encode. Whitespace is ignored so that
// pasted/wrapped text still works.
func Decode(s string) (ConnectData, error) {
	var d ConnectData

	raw, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		return d, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	if len(raw) < len(magic)+1 {
		return d, fmt.Errorf("%w: truncated header", ErrInvalid)
	}
	if !bytes.Equal(raw[:len(magic)], magic[:]) {
		return d, fmt.Errorf("%w: bad magic", ErrInvalid)
	}
	if raw[4] != Version {
		return d, fmt.Errorf("%w: unsupported version %d", ErrInvalid, raw[4])
	}

	rest := raw[5:]
	fields := make([]string, 3)
	for i := range fields {
		if len(rest) < 2 {
			return d, fmt.Errorf("%w: truncated length", ErrInvalid)
		}
		n := int(rest[0])<<8 | int(rest[1])
		rest = rest[2:]
		if n > maxFieldLen || len(rest) < n {
			return d, fmt.Errorf("%w: truncated field", ErrInvalid)
		}
		fields[i] = string(rest[:n])
		rest = rest[n:]
		if !utf8.ValidString(fields[i]) {
			return d, fmt.Errorf("%w: field is not valid utf-8", ErrInvalid)
		}
	}

	d = ConnectData{Room: fields[0], Name: fields[1], Fingerprint: fields[2]}
	if err := validate(d); err != nil {
		return ConnectData{}, err
	}
	return d, nil
}

// Group pretty-prints a blob in blocks of block hex chars to keep it readable
// in a terminal without breaking copy/paste (Decode ignores whitespace).
func Group(s string, block int) string {
	if block <= 0 {
		return s
	}

	var b strings.Builder
	for i := 0; i < len(s); i += block {
		if i > 0 {
			b.WriteByte(' ')
		}
		end := min(i+block, len(s))
		b.WriteString(s[i:end])
	}
	return b.String()
}

func validate(d ConnectData) error {
	if strings.TrimSpace(d.Room) == "" {
		return fmt.Errorf("%w: room is empty", ErrInvalid)
	}
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalid)
	}
	return nil
}

// RandomID returns n random bytes as lowercase hex. Useful for room names and
// peer identities.
func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable for us; fall back to a fixed
		// marker so callers still get a usable (if non-unique) value.
		for i := range b {
			b[i] = byte(i * 7)
		}
	}
	return hex.EncodeToString(b)
}
