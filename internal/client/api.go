package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Alice088/worm/internal/protocol"
)

type Session struct {
	UserID     string               `json:"user_id"`
	Name       string               `json:"name"`
	Token      string               `json:"token"`
	ExpiresAt  time.Time            `json:"expires_at"`
	WSURL      string               `json:"ws_url"`
	ICEServers []protocol.ICEServer `json:"ice_servers"`
	TURN       *Credential          `json:"turn,omitempty"`
}

type Credential struct {
	URLs      []string  `json:"urls"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	ExpiresAt time.Time `json:"expires_at"`
}

var ErrServerNotConfigured = errors.New("server not configured · press 6")

func NormalizeServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrServerNotConfigured
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server url: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	default:
		return "", fmt.Errorf("unsupported server scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("server host is empty")
	}
	path := strings.TrimSuffix(strings.TrimSuffix(parsed.Path, "/"), "/ws")
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func Register(ctx context.Context, baseURL, name string) (Session, error) {
	var session Session

	normalized, err := NormalizeServer(baseURL)
	if err != nil {
		return session, err
	}

	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return session, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalized+"/v1/session", bytes.NewReader(body))
	if err != nil {
		return session, err
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return session, fmt.Errorf("server unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return session, err
	}
	if resp.StatusCode != http.StatusOK {
		return session, fmt.Errorf("server rejected session: %s", serverError(data, resp.StatusCode))
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("malformed session response: %w", err)
	}
	if session.Token == "" || session.WSURL == "" {
		return session, errors.New("session response is missing token or ws_url")
	}
	return session, nil
}

func serverError(data []byte, status int) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return http.StatusText(status)
	}
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}
