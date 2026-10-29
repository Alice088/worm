package server

import (
	"strings"
	"testing"
	"time"
)

func envMap(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(envMap(map[string]string{"WORM_SECRET": "0123456789abcdef"}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Fatalf("unexpected listen %q", cfg.Listen)
	}
	if !cfg.TURN.Enabled {
		t.Fatal("turn should be enabled by default")
	}
	if cfg.TURN.Secret == "" {
		t.Fatal("turn secret should be derived from the signing secret")
	}
	if cfg.TokenTTL != 12*time.Hour {
		t.Fatalf("unexpected token ttl %v", cfg.TokenTTL)
	}
}

func TestLoadConfigRequiresSecret(t *testing.T) {
	if _, err := LoadConfig(envMap(map[string]string{})); err == nil {
		t.Fatal("missing secret must be rejected")
	}
	if _, err := LoadConfig(envMap(map[string]string{"WORM_SECRET": "short"})); err == nil {
		t.Fatal("short secret must be rejected")
	}
}

func TestLoadConfigParsesEnvironment(t *testing.T) {
	cfg, err := LoadConfig(envMap(map[string]string{
		"WORM_SECRET":           "0123456789abcdef",
		"WORM_LISTEN":           "127.0.0.1:9999",
		"WORM_PUBLIC_URL":       "https://calls.example.com/",
		"WORM_TOKEN_TTL":        "30m",
		"WORM_INVITE_TIMEOUT":   "45s",
		"WORM_CONNECT_TIMEOUT":  "12s",
		"WORM_RECONNECT_GRACE":  "5s",
		"WORM_TURN_URLS":        "turn:a:3478, turn:b:3478",
		"WORM_TURN_DISABLED":    "1",
		"WORM_ORIGINS":          "https://calls.example.com",
		"WORM_MAX_ACTIVE_CALLS": "7",
	}))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9999" {
		t.Fatalf("unexpected listen %q", cfg.Listen)
	}
	if cfg.PublicURL != "https://calls.example.com" {
		t.Fatalf("unexpected public url %q", cfg.PublicURL)
	}
	if cfg.TokenTTL != 30*time.Minute {
		t.Fatalf("unexpected ttl %v", cfg.TokenTTL)
	}
	if cfg.Call.InvitationTimeout != 45*time.Second || cfg.Call.ConnectionTimeout != 12*time.Second {
		t.Fatalf("unexpected call timeouts: %+v", cfg.Call)
	}
	if cfg.Signaling.ReconnectGrace != 5*time.Second {
		t.Fatalf("unexpected grace %v", cfg.Signaling.ReconnectGrace)
	}
	if len(cfg.TURN.URLs) != 2 || cfg.TURN.URLs[1] != "turn:b:3478" {
		t.Fatalf("unexpected turn urls: %+v", cfg.TURN.URLs)
	}
	if cfg.TURN.Enabled {
		t.Fatal("turn should be disabled")
	}
	if len(cfg.AllowedOrigins) != 1 {
		t.Fatalf("unexpected origins: %+v", cfg.AllowedOrigins)
	}
	if cfg.Call.MaxActiveCalls != 7 {
		t.Fatalf("unexpected max calls %d", cfg.Call.MaxActiveCalls)
	}
}

func TestLoadConfigRejectsInvalidDuration(t *testing.T) {
	_, err := LoadConfig(envMap(map[string]string{
		"WORM_SECRET":    "0123456789abcdef",
		"WORM_TOKEN_TTL": "forever",
	}))
	if err == nil || !strings.Contains(err.Error(), "WORM_TOKEN_TTL") {
		t.Fatalf("expected duration error, got %v", err)
	}
}

func TestValidateRejectsBadPublicURL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SigningSecret = "0123456789abcdef"
	cfg.PublicURL = "ftp://example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid public url must be rejected")
	}
}
