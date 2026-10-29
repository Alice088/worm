package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIssueVerifyRoundTrip(t *testing.T) {
	issuer, err := NewIssuer([]byte("0123456789abcdef"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	identity, token, err := issuer.Issue("alice")
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID == "" || identity.Name != "alice" {
		t.Fatalf("unexpected identity: %+v", identity)
	}

	back, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if back.ID != identity.ID || back.Name != identity.Name {
		t.Fatalf("identity mismatch: %+v != %+v", back, identity)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	issuer, _ := NewIssuer([]byte("0123456789abcdef"), time.Hour)
	_, token, _ := issuer.Issue("alice")

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("unexpected token format: %s", token)
	}
	tampered := parts[0] + "." + strings.Repeat("A", len(parts[1]))
	if _, err := issuer.Verify(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	if _, err := issuer.Verify("garbage"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	issuer, _ := NewIssuer([]byte("0123456789abcdef"), time.Minute)
	now := time.Now()
	issuer.SetClock(func() time.Time { return now })

	_, token, err := issuer.Issue("alice")
	if err != nil {
		t.Fatal(err)
	}
	issuer.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	if _, err := issuer.Verify(token); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestIssuerValidation(t *testing.T) {
	if _, err := NewIssuer([]byte("short"), time.Hour); err == nil {
		t.Fatal("short secret must be rejected")
	}
	if _, err := NewIssuer([]byte("0123456789abcdef"), 0); err == nil {
		t.Fatal("zero ttl must be rejected")
	}
}
