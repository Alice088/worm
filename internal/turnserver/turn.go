package turnserver

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pion/logging"
	pionturn "github.com/pion/turn/v5"
)

type Credential struct {
	URLs      []string  `json:"urls"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CredentialIssuer struct {
	secret []byte
	ttl    time.Duration
	urls   []string
	realm  string
	now    func() time.Time
}

func NewCredentialIssuer(secret string, ttl time.Duration, urls []string, realm string) (*CredentialIssuer, error) {
	if len(secret) < 16 {
		return nil, errors.New("turn secret must be at least 16 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("turn credential ttl must be positive")
	}
	if realm == "" {
		realm = "worm"
	}
	clean := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		clean = append(clean, u)
	}
	return &CredentialIssuer{
		secret: []byte(secret),
		ttl:    ttl,
		urls:   clean,
		realm:  realm,
		now:    time.Now,
	}, nil
}

func (i *CredentialIssuer) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	i.now = now
}

func (i *CredentialIssuer) URLs() []string {
	out := make([]string, len(i.urls))
	copy(out, i.urls)
	return out
}

func (i *CredentialIssuer) Issue(userID string) (Credential, error) {
	if strings.TrimSpace(userID) == "" {
		return Credential{}, errors.New("turn: empty user id")
	}
	exp := i.now().Add(i.ttl).UTC()
	username := strconv.FormatInt(exp.Unix(), 10) + ":" + userID
	return Credential{
		URLs:      i.URLs(),
		Username:  username,
		Password:  password(i.secret, username),
		ExpiresAt: exp,
	}, nil
}

func password(secret []byte, username string) string {
	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (i *CredentialIssuer) AuthHandler(ra *pionturn.RequestAttributes) (string, []byte, bool) {
	expText, user, ok := strings.Cut(ra.Username, ":")
	if !ok || user == "" {
		return "", nil, false
	}
	exp, err := strconv.ParseInt(expText, 10, 64)
	if err != nil || i.now().After(time.Unix(exp, 0)) {
		return "", nil, false
	}
	return ra.Username, pionturn.GenerateAuthKey(ra.Username, i.realm, password(i.secret, ra.Username)), true
}

type Hooks struct {
	AllocationCreated func()
	AllocationError   func()
}

type ServerConfig struct {
	ListenAddr    string
	PublicIP      string
	Realm         string
	Secret        string
	CredentialTTL time.Duration
	URLs          []string
	Logger        logging.LoggerFactory
	Hooks         Hooks
}

type Server struct {
	inner *pionturn.Server
	conn  net.PacketConn
	urls  []string
}

func Start(cfg ServerConfig) (*Server, error) {
	if cfg.Realm == "" {
		cfg.Realm = "worm"
	}
	if cfg.URLs == nil {
		cfg.URLs = []string{}
	}
	issuer, err := NewCredentialIssuer(cfg.Secret, cfg.CredentialTTL, cfg.URLs, cfg.Realm)
	if err != nil {
		return nil, err
	}

	addr := cfg.ListenAddr
	if addr == "" {
		addr = ":3478"
	}
	conn, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("turn: listen udp %s: %w", addr, err)
	}

	publicIP := net.ParseIP(cfg.PublicIP)
	if publicIP == nil || publicIP.IsUnspecified() {
		publicIP = derivePublicIP(conn.LocalAddr())
	}

	events := pionturn.EventHandler{}
	if cfg.Hooks.AllocationCreated != nil {
		events.OnAllocationCreated = func(_, _ net.Addr, _, _, _ string, _ net.Addr, _ int) {
			cfg.Hooks.AllocationCreated()
		}
	}
	if cfg.Hooks.AllocationError != nil {
		events.OnAllocationError = func(_, _ net.Addr, _, _ string) {
			cfg.Hooks.AllocationError()
		}
	}

	inner, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm:         cfg.Realm,
		LoggerFactory: cfg.Logger,
		AuthHandler:   issuer.AuthHandler,
		EventHandler:  events,
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn: conn,
			RelayAddressGenerator: &pionturn.RelayAddressGeneratorStatic{
				RelayAddress: publicIP,
				Address:      "0.0.0.0",
			},
		}},
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("turn: start server: %w", err)
	}

	urls := issuer.URLs()
	if len(urls) == 0 {
		urls = []string{"turn:" + net.JoinHostPort(publicIP.String(), port(conn.LocalAddr()))}
	}

	return &Server{inner: inner, conn: conn, urls: urls}, nil
}

func (s *Server) Addr() string {
	if s == nil || s.conn == nil {
		return ""
	}
	return s.conn.LocalAddr().String()
}

func (s *Server) URLs() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.urls))
	copy(out, s.urls)
	return out
}

func (s *Server) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func port(addr net.Addr) string {
	if tcp, ok := addr.(*net.UDPAddr); ok {
		return strconv.Itoa(tcp.Port)
	}
	_, p, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "3478"
	}
	return p
}

func derivePublicIP(addr net.Addr) net.IP {
	if udp, ok := addr.(*net.UDPAddr); ok && udp.IP != nil && !udp.IP.IsUnspecified() {
		return udp.IP
	}
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}
	defer func() { _ = conn.Close() }()
	if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return udp.IP
	}
	return net.ParseIP("127.0.0.1")
}
