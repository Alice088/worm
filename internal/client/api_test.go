package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"example.com:8080":         "http://example.com:8080",
		"http://example.com":       "http://example.com",
		"https://example.com/":     "https://example.com",
		"ws://example.com:8080/ws": "http://example.com:8080",
		"wss://example.com/ws":     "https://example.com",
		"http://example.com/v1/":   "http://example.com/v1",
	}
	for in, want := range cases {
		got, err := NormalizeServer(in)
		if err != nil {
			t.Fatalf("NormalizeServer(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("NormalizeServer(%q) = %q, want %q", in, got, want)
		}
	}

	if _, err := NormalizeServer(""); !errors.Is(err, ErrServerNotConfigured) {
		t.Fatalf("expected ErrServerNotConfigured, got %v", err)
	}
	if _, err := NormalizeServer("ftp://example.com"); err == nil {
		t.Fatal("unsupported scheme must be rejected")
	}
}

func TestRegister(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/session" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["name"] != "alice" {
			t.Errorf("unexpected name %q", body["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Session{
			UserID:    "u-1",
			Name:      "alice",
			Token:     "token",
			ExpiresAt: expires,
			WSURL:     "ws://example/ws",
		})
	}))
	defer srv.Close()

	sess, err := Register(context.Background(), srv.URL, "alice")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if sess.Token != "token" || sess.WSURL != "ws://example/ws" || sess.UserID != "u-1" {
		t.Fatalf("unexpected session: %+v", sess)
	}
}

func TestRegisterErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid name"}`))
	}))
	defer srv.Close()

	if _, err := Register(context.Background(), srv.URL, "x"); err == nil {
		t.Fatal("expected an error")
	} else if got := err.Error(); got != "server rejected session: invalid name" {
		t.Fatalf("unexpected error %q", got)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	defer broken.Close()
	if _, err := Register(context.Background(), broken.URL, "x"); err == nil {
		t.Fatal("expected a decode error")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer empty.Close()
	if _, err := Register(context.Background(), empty.URL, "x"); err == nil {
		t.Fatal("expected a missing field error")
	}
}
