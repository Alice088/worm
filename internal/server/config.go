package server

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Alice088/worm/internal/call"
)

type TURNConfig struct {
	Enabled       bool
	ListenAddr    string
	PublicIP      string
	Realm         string
	Secret        string
	CredentialTTL time.Duration
	URLs          []string
}

type Config struct {
	Listen          string
	PublicURL       string
	SigningSecret   string
	TokenTTL        time.Duration
	ShutdownTimeout time.Duration
	AllowedOrigins  []string
	InsecureOrigin  bool
	Call            call.Config
	Signaling       SignalingConfig
	TURN            TURNConfig
}

type SignalingConfig struct {
	SendQueue      int
	PendingQueue   int
	ReadLimit      int64
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MessageRate    float64
	MessageBurst   int
	CreateRate     float64
	CreateBurst    int
	ReconnectGrace time.Duration
}

func DefaultConfig() Config {
	return Config{
		Listen:          ":8080",
		TokenTTL:        12 * time.Hour,
		ShutdownTimeout: 15 * time.Second,
		Call:            call.DefaultConfig(),
		Signaling: SignalingConfig{
			SendQueue:      64,
			PendingQueue:   64,
			ReadLimit:      512 << 10,
			WriteTimeout:   10 * time.Second,
			IdleTimeout:    75 * time.Second,
			MessageRate:    40,
			MessageBurst:   60,
			CreateRate:     0.1,
			CreateBurst:    5,
			ReconnectGrace: 20 * time.Second,
		},
		TURN: TURNConfig{
			Enabled:       true,
			ListenAddr:    ":3478",
			Realm:         "worm",
			CredentialTTL: 10 * time.Minute,
		},
	}
}

func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()

	if v := getenv("WORM_LISTEN"); v != "" {
		cfg.Listen = v
	}
	cfg.PublicURL = strings.TrimRight(getenv("WORM_PUBLIC_URL"), "/")
	cfg.SigningSecret = getenv("WORM_SECRET")
	cfg.TURN.Secret = getenv("WORM_TURN_SECRET")
	cfg.TURN.PublicIP = getenv("WORM_TURN_PUBLIC_IP")
	cfg.TURN.Realm = firstNonEmpty(getenv("WORM_TURN_REALM"), cfg.TURN.Realm)
	if v := getenv("WORM_TURN_LISTEN"); v != "" {
		cfg.TURN.ListenAddr = v
	}
	if v := strings.TrimSpace(getenv("WORM_TURN_URLS")); v != "" {
		for _, u := range strings.Split(v, ",") {
			if u = strings.TrimSpace(u); u != "" {
				cfg.TURN.URLs = append(cfg.TURN.URLs, u)
			}
		}
	}
	if v := strings.TrimSpace(getenv("WORM_TURN_DISABLED")); v == "1" || strings.EqualFold(v, "true") {
		cfg.TURN.Enabled = false
	}
	if v := strings.TrimSpace(getenv("WORM_ORIGINS")); v != "" {
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
	}
	if v := strings.TrimSpace(getenv("WORM_ORIGINS_INSECURE")); v == "1" || strings.EqualFold(v, "true") {
		cfg.InsecureOrigin = true
	}

	var err error
	if cfg.TokenTTL, err = durationEnv(getenv, "WORM_TOKEN_TTL", cfg.TokenTTL); err != nil {
		return cfg, err
	}
	if cfg.ShutdownTimeout, err = durationEnv(getenv, "WORM_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return cfg, err
	}
	if cfg.Call.InvitationTimeout, err = durationEnv(getenv, "WORM_INVITE_TIMEOUT", cfg.Call.InvitationTimeout); err != nil {
		return cfg, err
	}
	if cfg.Call.ConnectionTimeout, err = durationEnv(getenv, "WORM_CONNECT_TIMEOUT", cfg.Call.ConnectionTimeout); err != nil {
		return cfg, err
	}
	if cfg.Call.Retention, err = durationEnv(getenv, "WORM_CALL_RETENTION", cfg.Call.Retention); err != nil {
		return cfg, err
	}
	if cfg.Signaling.ReconnectGrace, err = durationEnv(getenv, "WORM_RECONNECT_GRACE", cfg.Signaling.ReconnectGrace); err != nil {
		return cfg, err
	}
	if cfg.TURN.CredentialTTL, err = durationEnv(getenv, "WORM_TURN_CRED_TTL", cfg.TURN.CredentialTTL); err != nil {
		return cfg, err
	}
	if cfg.Call.MaxActiveCalls, err = intEnv(getenv, "WORM_MAX_ACTIVE_CALLS", cfg.Call.MaxActiveCalls); err != nil {
		return cfg, err
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	switch {
	case strings.TrimSpace(c.Listen) == "":
		return errors.New("listen address is required")
	case len(c.SigningSecret) < 16:
		return errors.New("WORM_SECRET must be set and at least 16 characters long")
	case c.TokenTTL <= 0:
		return errors.New("token ttl must be positive")
	case c.ShutdownTimeout <= 0:
		return errors.New("shutdown timeout must be positive")
	case c.Signaling.ReconnectGrace <= 0:
		return errors.New("reconnect grace must be positive")
	}
	if err := c.Call.Validate(); err != nil {
		return fmt.Errorf("call settings: %w", err)
	}
	if c.PublicURL != "" && !strings.HasPrefix(c.PublicURL, "http://") && !strings.HasPrefix(c.PublicURL, "https://") {
		return errors.New("WORM_PUBLIC_URL must start with http:// or https://")
	}
	if c.TURN.Enabled {
		if c.TURN.Secret == "" {
			c.TURN.Secret = c.SigningSecret + "/turn"
		}
		if len(c.TURN.Secret) < 16 {
			return errors.New("turn secret must be at least 16 characters")
		}
		if c.TURN.CredentialTTL <= 0 {
			return errors.New("turn credential ttl must be positive")
		}
		if strings.TrimSpace(c.TURN.Realm) == "" {
			return errors.New("turn realm must not be empty")
		}
	}
	return nil
}

func durationEnv(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", key, err)
	}
	if d <= 0 {
		return fallback, fmt.Errorf("%s must be positive", key)
	}
	return d, nil
}

func intEnv(getenv func(string) string, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func Getenv(key string) string { return os.Getenv(key) }
