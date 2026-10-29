package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

type Identity struct {
	ID        string    `json:"sub"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"exp"`
}

type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
	newID  func() (string, error)
}

func NewIssuer(secret []byte, ttl time.Duration) (*Issuer, error) {
	if len(secret) < 16 {
		return nil, errors.New("auth secret must be at least 16 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("token ttl must be positive")
	}
	key := make([]byte, len(secret))
	copy(key, secret)
	return &Issuer{secret: key, ttl: ttl, now: time.Now, newID: randomID}, nil
}

func (i *Issuer) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	i.now = now
}

func (i *Issuer) TTL() time.Duration { return i.ttl }

func (i *Issuer) Issue(name string) (Identity, string, error) {
	id, err := i.newID()
	if err != nil {
		return Identity{}, "", err
	}
	identity := Identity{ID: id, Name: name, ExpiresAt: i.now().Add(i.ttl).UTC()}
	token, err := i.sign(identity)
	if err != nil {
		return Identity{}, "", err
	}
	return identity, token, nil
}

func (i *Issuer) Verify(token string) (Identity, error) {
	var id Identity

	body, macText, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || body == "" || macText == "" {
		return id, ErrInvalidToken
	}
	mac, err := base64.RawURLEncoding.DecodeString(macText)
	if err != nil {
		return id, ErrInvalidToken
	}
	if !hmac.Equal(mac, i.signature(body)) {
		return id, ErrInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return id, ErrInvalidToken
	}
	if err := json.Unmarshal(raw, &id); err != nil {
		return id, ErrInvalidToken
	}
	if id.ID == "" || id.Name == "" {
		return id, ErrInvalidToken
	}
	if !i.now().Before(id.ExpiresAt) {
		return id, ErrExpiredToken
	}
	return id, nil
}

func (i *Issuer) sign(id Identity) (string, error) {
	raw, err := json.Marshal(id)
	if err != nil {
		return "", fmt.Errorf("encode identity: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(i.signature(body)), nil
}

func (i *Issuer) signature(body string) []byte {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

type Authenticator interface {
	Verify(token string) (Identity, error)
}

var _ Authenticator = (*Issuer)(nil)
