package call

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestManager(t *testing.T) (*Manager, *testClock) {
	t.Helper()

	cfg := DefaultConfig()
	cfg.InvitationTimeout = 30 * time.Second
	cfg.ConnectionTimeout = 20 * time.Second
	cfg.Retention = 2 * time.Minute

	m, err := NewManager(cfg, []byte("test-invite-secret-0123456789"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	clock := &testClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m.SetClock(clock.Now)

	var n int
	var mu sync.Mutex
	m.SetIDGenerator(func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return "call-" + string(rune('a'+n-1)), nil
	})

	return m, clock
}

var (
	alice = Party{ID: "u-alice", Name: "alice"}
	bob   = Party{ID: "u-bob", Name: "bob"}
	carol = Party{ID: "u-carol", Name: "carol"}
)

func createCall(t *testing.T, m *Manager) Snapshot {
	t.Helper()
	snap, err := m.Create(alice, "", "req-create")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return snap
}

func TestCreateRingingCall(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if snap.State != StateRinging {
		t.Fatalf("expected ringing, got %s", snap.State)
	}
	if snap.Invite == "" {
		t.Fatal("expected an invite token")
	}
	if snap.Caller != alice {
		t.Fatalf("unexpected caller: %+v", snap.Caller)
	}
	if snap.Callee.ID != "" {
		t.Fatal("callee must not be bound before invite acceptance")
	}
	if snap.Deadline.IsZero() {
		t.Fatal("expected an invitation deadline")
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	m, _ := newTestManager(t)

	first, err := m.Create(alice, "", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Create(alice, "", "req-1")
	if err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate create produced a new call: %s != %s", first.ID, second.ID)
	}
	if m.Active() != 1 {
		t.Fatalf("expected one active call, got %d", m.Active())
	}
}

func TestRequestIDReuseIsRejected(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if _, err := m.Accept(alice, snap.ID, snap.Invite, "req-create"); !errors.Is(err, ErrRequestReuse) {
		t.Fatalf("expected ErrRequestReuse, got %v", err)
	}
}

func TestAcceptRequiresValidInvite(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if _, err := m.Accept(bob, snap.ID, "forged-signature", "req-accept"); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("expected ErrInviteInvalid, got %v", err)
	}
	if _, err := m.Accept(carol, snap.ID, snap.Invite, "req-accept-2"); err != nil {
		t.Fatalf("accept with valid invite: %v", err)
	}
	got, err := m.Get(carol.ID, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateAccepted || got.Callee.ID != carol.ID {
		t.Fatalf("unexpected accepted state: %+v", got)
	}
}

func TestCallerCannotAcceptOwnCall(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if _, err := m.Accept(alice, snap.ID, snap.Invite, "req"); !errors.Is(err, ErrSelfCall) {
		t.Fatalf("expected ErrSelfCall, got %v", err)
	}
}

func TestAcceptIsIdempotent(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	first, err := m.Accept(bob, snap.ID, snap.Invite, "req-accept")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Accept(bob, snap.ID, "", "req-accept")
	if err != nil {
		t.Fatalf("duplicate accept: %v", err)
	}
	if second.State != first.State || second.Callee.ID != bob.ID {
		t.Fatalf("duplicate accept changed state: %+v", second)
	}
}

func TestRejectOnlyByCallee(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if _, err := m.Reject(alice, snap.ID, "", "no"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("caller must not reject: %v", err)
	}
	got, err := m.Reject(bob, snap.ID, snap.Invite, "declined")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateRejected || got.Reason != "declined" {
		t.Fatalf("unexpected reject result: %+v", got)
	}
	if _, err := m.Accept(carol, snap.ID, snap.Invite, "req-late"); err == nil {
		t.Fatal("accept after terminal state must fail")
	}
}

func TestCancelOnlyByCallerWhileRinging(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if _, err := m.Cancel(bob.ID, snap.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member must not cancel: %v", err)
	}
	got, err := m.Cancel(alice.ID, snap.ID, "changed my mind")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateCancelled {
		t.Fatalf("expected cancelled, got %s", got.State)
	}
	if _, err := m.Cancel(alice.ID, snap.ID, "again"); err == nil {
		t.Fatal("second cancel must not transition")
	}
}

func TestEndIsIdempotentAndTerminal(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "a"); err != nil {
		t.Fatal(err)
	}

	first, err := m.End(alice.ID, snap.ID, "hangup")
	if err != nil {
		t.Fatal(err)
	}
	if first.State != StateEnded {
		t.Fatalf("expected ended, got %s", first.State)
	}
	second, err := m.End(bob.ID, snap.ID, "hangup")
	if err != nil {
		t.Fatalf("simultaneous hangup should be safe: %v", err)
	}
	if second.State != StateEnded {
		t.Fatalf("expected ended, got %s", second.State)
	}
	if _, err := m.Accept(bob, snap.ID, "", "late"); err == nil {
		t.Fatal("terminal calls must be immutable")
	}
}

func TestConcurrentHangup(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "a"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, user := range []string{alice.ID, bob.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := m.End(id, snap.ID, "hangup")
			errs <- err
		}(user)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent end failed: %v", err)
		}
	}

	got, err := m.Get(alice.ID, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateEnded {
		t.Fatalf("expected ended, got %s", got.State)
	}
}

func TestConnectedRequiresBothReports(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "a"); err != nil {
		t.Fatal(err)
	}

	connecting, err := m.BeginNegotiation(alice.ID, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if connecting.State != StateConnecting {
		t.Fatalf("expected connecting, got %s", connecting.State)
	}

	one, err := m.ReportReady(alice.ID, snap.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if one.State != StateConnecting {
		t.Fatalf("must not connect on one report: %s", one.State)
	}

	both, err := m.ReportReady(bob.ID, snap.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if both.State != StateConnected {
		t.Fatalf("expected connected, got %s", both.State)
	}
	if both.ConnectedAt.IsZero() {
		t.Fatal("expected connected timestamp")
	}

	degraded, err := m.ReportReady(bob.ID, snap.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if degraded.State != StateConnecting {
		t.Fatalf("expected fallback to connecting, got %s", degraded.State)
	}
}

func TestSdpNegotiationDoesNotConnect(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "a"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := m.BeginNegotiation(alice.ID, snap.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == StateConnected {
			t.Fatal("negotiation must not mark the call connected")
		}
	}
}

func TestInvitationTimeout(t *testing.T) {
	m, clock := newTestManager(t)
	snap := createCall(t, m)

	clock.Advance(31 * time.Second)
	expired := m.Sweep()
	if len(expired) != 1 {
		t.Fatalf("expected one timeout snapshot, got %d", len(expired))
	}
	if expired[0].State != StateTimeout {
		t.Fatalf("expected timeout, got %s", expired[0].State)
	}
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "late"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestConnectionTimeout(t *testing.T) {
	m, clock := newTestManager(t)
	snap := createCall(t, m)
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "a"); err != nil {
		t.Fatal(err)
	}

	clock.Advance(21 * time.Second)
	failed := m.Sweep()
	if len(failed) != 1 || failed[0].State != StateFailed {
		t.Fatalf("expected one failed snapshot, got %+v", failed)
	}
}

func TestInviteExpiryIsEnforced(t *testing.T) {
	m, clock := newTestManager(t)
	snap := createCall(t, m)

	clock.Advance(29 * time.Second)
	if _, err := m.Accept(bob, snap.ID, snap.Invite, "ok"); err != nil {
		t.Fatalf("accept within deadline: %v", err)
	}
}

func TestMembershipIsAuthorized(t *testing.T) {
	m, _ := newTestManager(t)
	snap := createCall(t, m)

	if _, err := m.Get(carol.ID, snap.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider must not read the call: %v", err)
	}
	if _, err := m.End(carol.ID, snap.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider must not end the call: %v", err)
	}
	if _, err := m.Get(alice.ID, "unknown-call"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown call must be not found: %v", err)
	}
}

func TestOneActiveCallPerParticipant(t *testing.T) {
	m, _ := newTestManager(t)
	first := createCall(t, m)

	if _, err := m.Create(alice, "", "req-second"); !errors.Is(err, ErrAlreadyInCall) {
		t.Fatalf("expected ErrAlreadyInCall, got %v", err)
	}
	if _, err := m.Accept(bob, first.ID, first.Invite, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(bob, "", "req-third"); !errors.Is(err, ErrAlreadyInCall) {
		t.Fatalf("busy callee must not create a call: %v", err)
	}
}

func TestDirectCallRequiresOnlineCallee(t *testing.T) {
	m, _ := newTestManager(t)
	snap, err := m.Create(alice, bob.ID, "req")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Callee.ID != bob.ID {
		t.Fatalf("expected callee to be bound: %+v", snap.Callee)
	}
	if _, err := m.Create(alice, alice.ID, "req-self"); !errors.Is(err, ErrSelfCall) {
		t.Fatalf("expected ErrSelfCall, got %v", err)
	}
}

func TestRetentionRemovesTerminalCalls(t *testing.T) {
	m, clock := newTestManager(t)
	snap := createCall(t, m)
	if _, err := m.Cancel(alice.ID, snap.ID, "x"); err != nil {
		t.Fatal(err)
	}

	clock.Advance(3 * time.Minute)
	m.Sweep()
	if _, err := m.Get(alice.ID, snap.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected retention cleanup, got %v", err)
	}
}

func TestConcurrentCreateAcceptEnd(t *testing.T) {
	m, _ := newTestManager(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			caller := Party{ID: "user-" + string(rune('a'+i)), Name: "u"}
			snap, err := m.Create(caller, "", "")
			if err != nil {
				return
			}
			if _, err := m.Accept(Party{ID: caller.ID + "-peer", Name: "p"}, snap.ID, snap.Invite, ""); err != nil {
				return
			}
			_, _ = m.End(caller.ID, snap.ID, "x")
		}(i)
	}
	wg.Wait()

	if active := m.Active(); active != 0 {
		t.Fatalf("expected no active calls, got %d", active)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config rejected: %v", err)
	}
	cfg.ConnectionTimeout = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("zero timeout must be rejected")
	}
	if _, err := NewManager(DefaultConfig(), []byte("short")); err == nil {
		t.Fatal("short invite secret must be rejected")
	}
}
