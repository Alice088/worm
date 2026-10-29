package session

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var signalMagic = [4]byte{'W', 'R', 'M', 0x03}

const (
	SignalOffer      = "offer"
	SignalAnswer     = "answer"
	SignalCandidates = "candidates"

	maxSignalField = 200_000
)

type Signal struct {
	Kind       string
	SDP        string
	Candidates []string
}

func (s Signal) Encode() (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	candidates := strings.Join(s.Candidates, "\n")

	var buf bytes.Buffer
	buf.Write(signalMagic[:])
	for _, field := range []string{s.Kind, s.SDP, candidates} {
		if len(field) > maxSignalField {
			return "", fmt.Errorf("%w: field too long", ErrInvalid)
		}
		buf.WriteByte(byte(len(field) >> 8))
		buf.WriteByte(byte(len(field)))
		buf.WriteString(field)
	}
	return hex.EncodeToString(buf.Bytes()), nil
}

func DecodeSignal(code string) (Signal, error) {
	var sig Signal

	raw, err := hex.DecodeString(strings.Join(strings.Fields(code), ""))
	if err != nil {
		return sig, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(raw) < len(signalMagic) {
		return sig, fmt.Errorf("%w: truncated header", ErrInvalid)
	}
	if !bytes.Equal(raw[:len(signalMagic)], signalMagic[:]) {
		return sig, fmt.Errorf("%w: wrong code kind", ErrInvalid)
	}

	rest := raw[len(signalMagic):]
	fields := make([]string, 3)
	for i := range fields {
		if len(rest) < 2 {
			return sig, fmt.Errorf("%w: truncated length", ErrInvalid)
		}
		n := int(rest[0])<<8 | int(rest[1])
		rest = rest[2:]
		if n > maxSignalField || len(rest) < n {
			return sig, fmt.Errorf("%w: truncated field", ErrInvalid)
		}
		fields[i] = string(rest[:n])
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return sig, fmt.Errorf("%w: trailing data", ErrInvalid)
	}

	sig = Signal{Kind: fields[0], SDP: fields[1]}
	if fields[2] != "" {
		for _, c := range strings.Split(fields[2], "\n") {
			if c = strings.TrimSpace(c); c != "" {
				sig.Candidates = append(sig.Candidates, c)
			}
		}
	}
	if err := sig.validate(); err != nil {
		return Signal{}, err
	}
	return sig, nil
}

func (s Signal) validate() error {
	switch s.Kind {
	case SignalOffer, SignalAnswer:
		if strings.TrimSpace(s.SDP) == "" {
			return fmt.Errorf("%w: sdp is empty", ErrInvalid)
		}
	case SignalCandidates:
		if len(s.Candidates) == 0 {
			return fmt.Errorf("%w: no candidates", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown code kind", ErrInvalid)
	}
	if !strings.Contains(s.SDP, "v=") && s.Kind != SignalCandidates {
		return fmt.Errorf("%w: sdp is malformed", ErrInvalid)
	}
	return nil
}

var ErrSignalKind = errors.New("wrong signal kind")
