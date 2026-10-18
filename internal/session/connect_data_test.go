package session

import (
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []ConnectData{
		{Room: "room-abc", Name: "alice"},
		{Room: "r", Name: "b"},
		{Room: "room-0123456789abcdef", Name: "peer-01", Fingerprint: "AA:BB:CC:DD"},
		{Room: "кириллица", Name: "имя"},
	}

	for _, want := range cases {
		blob, err := Encode(want)
		if err != nil {
			t.Fatalf("Encode(%+v): %v", want, err)
		}

		got, err := Decode(blob)
		if err != nil {
			t.Fatalf("Decode(%q): %v", blob, err)
		}
		if got != want {
			t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
		}
	}
}

func TestEncodeIsHexAndStable(t *testing.T) {
	d := ConnectData{Room: "room-1", Name: "dev"}

	blob, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(blob) != blob {
		t.Fatalf("expected lowercase hex, got %q", blob)
	}
	for _, r := range blob {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("non-hex rune %q in %q", r, blob)
		}
	}

	again, _ := Encode(d)
	if again != blob {
		t.Fatalf("encoding is not deterministic: %q != %q", blob, again)
	}
}

func TestDecodeAcceptsGroupedInput(t *testing.T) {
	d := ConnectData{Room: "room-xyz", Name: "bob"}
	blob, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}

	// Whitespace and newlines are ignored, so a wrapped paste still decodes.
	wrapped := Group(blob, 8)
	if !strings.Contains(wrapped, " ") {
		t.Fatalf("Group did not insert separators: %q", wrapped)
	}

	got, err := Decode("  \n" + wrapped + "\n")
	if err != nil {
		t.Fatalf("Decode wrapped: %v", err)
	}
	if got != d {
		t.Fatalf("got %+v want %+v", got, d)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	bad := []string{
		"",
		"zz",
		"deadbeef",
		"57524d",                     // magic only, no version
		"57524d99",                   // bad version
		"57524d01" + "0005" + "6162", // truncated field
	}

	for _, s := range bad {
		if _, err := Decode(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestEncodeRejectsEmptyFields(t *testing.T) {
	if _, err := Encode(ConnectData{Name: "x"}); err == nil {
		t.Fatal("expected error for empty room")
	}
	if _, err := Encode(ConnectData{Room: "x"}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRandomID(t *testing.T) {
	a, b := RandomID(8), RandomID(8)
	if len(a) != 16 || len(b) != 16 {
		t.Fatalf("expected 16 hex chars, got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Fatal("expected distinct random ids")
	}
}
