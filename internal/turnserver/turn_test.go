package turnserver

import (
	"net"
	"testing"
	"time"

	pionturn "github.com/pion/turn/v5"
)

const testSecret = "turn-test-secret-0123456789"

func newIssuer(t *testing.T) *CredentialIssuer {
	t.Helper()
	issuer, err := NewCredentialIssuer(testSecret, 5*time.Minute, []string{"turn:127.0.0.1:3478"}, "worm")
	if err != nil {
		t.Fatalf("NewCredentialIssuer: %v", err)
	}
	return issuer
}

func TestIssueCredentials(t *testing.T) {
	issuer := newIssuer(t)
	cred, err := issuer.Issue("user-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if cred.Username == "" || cred.Password == "" {
		t.Fatalf("empty credential: %+v", cred)
	}
	if len(cred.URLs) != 1 || cred.URLs[0] != "turn:127.0.0.1:3478" {
		t.Fatalf("unexpected urls: %+v", cred.URLs)
	}
	if !cred.ExpiresAt.After(time.Now()) {
		t.Fatalf("credential is already expired: %v", cred.ExpiresAt)
	}
}

func TestAuthHandlerAcceptsValidUsername(t *testing.T) {
	issuer := newIssuer(t)
	cred, _ := issuer.Issue("user-1")

	user, key, ok := issuer.AuthHandler(&pionturn.RequestAttributes{Username: cred.Username})
	if !ok {
		t.Fatal("valid credentials rejected")
	}
	if user != cred.Username || len(key) == 0 {
		t.Fatalf("unexpected auth result: %q %d", user, len(key))
	}
}

func TestAuthHandlerRejectsExpiredAndMalformed(t *testing.T) {
	issuer := newIssuer(t)
	cred, _ := issuer.Issue("user-1")

	now := time.Now()
	issuer.SetClock(func() time.Time { return now.Add(10 * time.Minute) })
	if _, _, ok := issuer.AuthHandler(&pionturn.RequestAttributes{Username: cred.Username}); ok {
		t.Fatal("expired credentials accepted")
	}

	issuer.SetClock(func() time.Time { return now })
	if _, _, ok := issuer.AuthHandler(&pionturn.RequestAttributes{Username: "no-colon"}); ok {
		t.Fatal("malformed username accepted")
	}
	if _, _, ok := issuer.AuthHandler(&pionturn.RequestAttributes{Username: "123:"}); ok {
		t.Fatal("empty user accepted")
	}
}

func TestServerAllocatesRelay(t *testing.T) {
	issuer := newIssuer(t)
	srv, err := Start(ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		PublicIP:      "127.0.0.1",
		Realm:         "worm",
		Secret:        testSecret,
		CredentialTTL: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Close() }()

	cred, err := issuer.Issue("user-relay")
	if err != nil {
		t.Fatal(err)
	}

	relay := allocate(t, srv.Addr(), "worm", cred.Username, cred.Password)
	if relay == nil {
		t.Fatal("expected a relay allocation")
	}
	_ = relay.Close()
}

func TestServerRejectsBadPassword(t *testing.T) {
	srv, err := Start(ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		PublicIP:      "127.0.0.1",
		Realm:         "worm",
		Secret:        testSecret,
		CredentialTTL: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Close() }()

	issuer := newIssuer(t)
	cred, _ := issuer.Issue("user-bad")

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, err := pionturn.NewClient(&pionturn.ClientConfig{
		STUNServerAddr: srv.Addr(),
		TURNServerAddr: srv.Addr(),
		Conn:           conn,
		Username:       cred.Username,
		Password:       "wrong-password",
		Realm:          "worm",
	})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Listen(); err != nil {
		_ = conn.Close()
		t.Fatalf("Listen: %v", err)
	}
	defer client.Close()

	if _, err := client.Allocate(); err == nil {
		t.Fatal("allocation with bad password must fail")
	}
}

func allocate(t *testing.T, addr, realm, username, password string) net.PacketConn {
	t.Helper()

	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, err := pionturn.NewClient(&pionturn.ClientConfig{
		STUNServerAddr: addr,
		TURNServerAddr: addr,
		Conn:           conn,
		Username:       username,
		Password:       password,
		Realm:          realm,
	})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Listen(); err != nil {
		_ = conn.Close()
		t.Fatalf("Listen: %v", err)
	}

	relay, err := client.Allocate()
	if err != nil {
		client.Close()
		_ = conn.Close()
		t.Fatalf("Allocate: %v", err)
	}
	return relay
}

func TestStartRejectsBadSecret(t *testing.T) {
	if _, err := Start(ServerConfig{ListenAddr: "127.0.0.1:0", Secret: "short"}); err == nil {
		t.Fatal("short secret must be rejected")
	}
}
