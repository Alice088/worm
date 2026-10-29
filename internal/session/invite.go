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

var magic = [4]byte{'W', 'R', 'M', 0x02}

const (
	maxFieldLen = 1024
	maxFields   = 4
)

type Invite struct {
	Server string
	CallID string
	Token  string
	Caller string
}

var ErrInvalid = errors.New("invalid invite code")

func (i Invite) Encode() (string, error) {
	if err := i.validate(); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	buf.Write(magic[:])
	for _, field := range []string{i.Server, i.CallID, i.Token, i.Caller} {
		if len(field) > maxFieldLen {
			return "", fmt.Errorf("%w: field too long", ErrInvalid)
		}
		buf.WriteByte(byte(len(field) >> 8))
		buf.WriteByte(byte(len(field)))
		buf.WriteString(field)
	}
	return hex.EncodeToString(buf.Bytes()), nil
}

func DecodeInvite(s string) (Invite, error) {
	var inv Invite

	raw, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		return inv, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(raw) < len(magic) {
		return inv, fmt.Errorf("%w: truncated header", ErrInvalid)
	}
	if !bytes.Equal(raw[:len(magic)], magic[:]) {
		return inv, fmt.Errorf("%w: bad magic", ErrInvalid)
	}

	rest := raw[len(magic):]
	fields := make([]string, maxFields)
	for i := range fields {
		if len(rest) < 2 {
			return inv, fmt.Errorf("%w: truncated length", ErrInvalid)
		}
		n := int(rest[0])<<8 | int(rest[1])
		rest = rest[2:]
		if n > maxFieldLen || len(rest) < n {
			return inv, fmt.Errorf("%w: truncated field", ErrInvalid)
		}
		fields[i] = string(rest[:n])
		rest = rest[n:]
		if !utf8.ValidString(fields[i]) {
			return inv, fmt.Errorf("%w: field is not valid utf-8", ErrInvalid)
		}
	}
	if len(rest) != 0 {
		return inv, fmt.Errorf("%w: trailing data", ErrInvalid)
	}

	inv = Invite{Server: fields[0], CallID: fields[1], Token: fields[2], Caller: fields[3]}
	if err := inv.validate(); err != nil {
		return Invite{}, err
	}
	return inv, nil
}

func (i Invite) validate() error {
	switch {
	case strings.TrimSpace(i.Server) == "":
		return fmt.Errorf("%w: server is empty", ErrInvalid)
	case strings.TrimSpace(i.CallID) == "":
		return fmt.Errorf("%w: call id is empty", ErrInvalid)
	case strings.TrimSpace(i.Token) == "":
		return fmt.Errorf("%w: invite token is empty", ErrInvalid)
	}
	return nil
}

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

func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(i * 7)
		}
	}
	return hex.EncodeToString(b)
}
