package session

import (
	"errors"
	"strings"
	"testing"
)

func TestInviteRoundTrip(t *testing.T) {
	inv := Invite{
		Server: "https://worm.example:8443",
		CallID: "9f2c3a4b5d6e",
		Token:  "payload.signature",
		Caller: "alice",
	}

	code, err := inv.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if code == "" {
		t.Fatal("empty invite code")
	}

	back, err := DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite: %v", err)
	}
	if back != inv {
		t.Fatalf("roundtrip mismatch: %+v != %+v", back, inv)
	}
}

func TestInviteIgnoresWhitespace(t *testing.T) {
	inv := Invite{Server: "http://localhost:8080", CallID: "c1", Token: "t", Caller: "bob"}
	code, _ := inv.Encode()

	var spaced strings.Builder
	for i, r := range code {
		if i > 0 && i%16 == 0 {
			spaced.WriteByte(' ')
		}
		spaced.WriteRune(r)
	}

	back, err := DecodeInvite(spaced.String())
	if err != nil {
		t.Fatalf("DecodeInvite grouped: %v", err)
	}
	if back.CallID != inv.CallID {
		t.Fatalf("unexpected call id %q", back.CallID)
	}
}

func TestInviteRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"deadbeef",
		"zzzz",
		"57524d020010 6161",
	}
	for _, c := range cases {
		if _, err := DecodeInvite(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestInviteValidation(t *testing.T) {
	if _, err := (Invite{CallID: "c", Token: "t"}).Encode(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty server, got %v", err)
	}
	if _, err := (Invite{Server: "s", Token: "t"}).Encode(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty call id, got %v", err)
	}
}

func TestGroup(t *testing.T) {
	if got := Group("abcdef", 2); got != "ab cd ef" {
		t.Fatalf("unexpected group: %q", got)
	}
	if got := Group("abc", 0); got != "abc" {
		t.Fatalf("unexpected group: %q", got)
	}
}

func TestRandomID(t *testing.T) {
	a, b := RandomID(8), RandomID(8)
	if len(a) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(a))
	}
	if a == b {
		t.Fatal("random ids must differ")
	}
}
