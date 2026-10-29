package call

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateRinging    State = "ringing"
	StateAccepted   State = "accepted"
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateEnded      State = "ended"
	StateRejected   State = "rejected"
	StateCancelled  State = "cancelled"
	StateTimeout    State = "timeout"
	StateFailed     State = "failed"
)

func (s State) Terminal() bool {
	switch s {
	case StateEnded, StateRejected, StateCancelled, StateTimeout, StateFailed:
		return true
	}
	return false
}

func (s State) Valid() bool {
	switch s {
	case StateRinging, StateAccepted, StateConnecting, StateConnected,
		StateEnded, StateRejected, StateCancelled, StateTimeout, StateFailed:
		return true
	}
	return false
}

type Role string

const (
	RoleCaller Role = "caller"
	RoleCallee Role = "callee"
)

type Party struct {
	ID   string
	Name string
}

func (p Party) Valid() bool { return p.ID != "" }

type Snapshot struct {
	ID          string
	State       State
	Caller      Party
	Callee      Party
	CreatedAt   time.Time
	Deadline    time.Time
	ConnectedAt time.Time
	TerminalAt  time.Time
	Reason      string
	Invite      string
}

func (s Snapshot) RoleOf(userID string) (Role, bool) {
	switch userID {
	case "":
		return "", false
	case s.Caller.ID:
		return RoleCaller, true
	case s.Callee.ID:
		return RoleCallee, true
	}
	return "", false
}

func (s Snapshot) Participant(userID string) (Party, Role, bool) {
	role, ok := s.RoleOf(userID)
	if !ok {
		return Party{}, "", false
	}
	if role == RoleCaller {
		return s.Caller, role, true
	}
	return s.Callee, role, true
}

func (s Snapshot) Peer(userID string) (Party, bool) {
	role, ok := s.RoleOf(userID)
	if !ok {
		return Party{}, false
	}
	if role == RoleCaller {
		if s.Callee.ID == "" {
			return Party{}, false
		}
		return s.Callee, true
	}
	return s.Caller, true
}

func (s Snapshot) Members() []string {
	members := make([]string, 0, 2)
	if s.Caller.ID != "" {
		members = append(members, s.Caller.ID)
	}
	if s.Callee.ID != "" {
		members = append(members, s.Callee.ID)
	}
	return members
}

var (
	ErrNotFound      = errors.New("call not found")
	ErrForbidden     = errors.New("forbidden")
	ErrInviteInvalid = errors.New("invalid invite")
	ErrExpired       = errors.New("invite expired")
	ErrRequestReuse  = errors.New("request_id reused for a different operation")
	ErrSelfCall      = errors.New("cannot call yourself")
	ErrTooManyCalls  = errors.New("too many active calls")
	ErrUnknownParty  = errors.New("unknown party")
	ErrAlreadyInCall = errors.New("participant is already in another call")
)

type StateError struct {
	From State
	To   State
}

func (e *StateError) Error() string {
	if e.To == "" {
		return fmt.Sprintf("call is in %s state", e.From)
	}
	return fmt.Sprintf("transition %s -> %s is not allowed", e.From, e.To)
}

func (e *StateError) Is(target error) bool {
	_, ok := target.(*StateError)
	return ok
}

type Config struct {
	InvitationTimeout time.Duration
	ConnectionTimeout time.Duration
	Retention         time.Duration
	IdempotencyTTL    time.Duration
	MaxActiveCalls    int
}

func DefaultConfig() Config {
	return Config{
		InvitationTimeout: 60 * time.Second,
		ConnectionTimeout: 30 * time.Second,
		Retention:         5 * time.Minute,
		IdempotencyTTL:    2 * time.Minute,
		MaxActiveCalls:    10_000,
	}
}

func (c Config) Validate() error {
	switch {
	case c.InvitationTimeout <= 0:
		return errors.New("invitation timeout must be positive")
	case c.ConnectionTimeout <= 0:
		return errors.New("connection timeout must be positive")
	case c.Retention <= 0:
		return errors.New("retention must be positive")
	case c.IdempotencyTTL <= 0:
		return errors.New("idempotency ttl must be positive")
	case c.MaxActiveCalls <= 0:
		return errors.New("max active calls must be positive")
	}
	return nil
}

type Manager struct {
	cfg          Config
	inviteSecret []byte
	calls        map[string]*call
	userCalls    map[string]string
	idem         map[string]idemRecord
	pending      map[string]Snapshot
	pendingOrder []string
	now          func() time.Time
	newID        func() (string, error)
	mu           sync.Mutex
}

type idemRecord struct {
	userID  string
	op      string
	callID  string
	result  Snapshot
	expires time.Time
}

func NewManager(cfg Config, inviteSecret []byte) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("call config: %w", err)
	}
	if len(inviteSecret) < 16 {
		return nil, errors.New("invite secret must be at least 16 bytes")
	}
	secret := make([]byte, len(inviteSecret))
	copy(secret, inviteSecret)

	return &Manager{
		cfg:          cfg,
		inviteSecret: secret,
		calls:        make(map[string]*call),
		userCalls:    make(map[string]string),
		idem:         make(map[string]idemRecord),
		pending:      make(map[string]Snapshot),
		now:          time.Now,
		newID:        randomCallID,
	}, nil
}

func (m *Manager) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = now
}

func (m *Manager) SetIDGenerator(fn func() (string, error)) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.newID = fn
}

func (m *Manager) Create(caller Party, calleeID, requestID string) (Snapshot, error) {
	if !caller.Valid() {
		return Snapshot{}, ErrUnknownParty
	}
	calleeID = strings.TrimSpace(calleeID)
	if calleeID != "" && calleeID == caller.ID {
		return Snapshot{}, ErrSelfCall
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if snap, ok, err := m.replayLocked(caller.ID, "create", requestID, now); ok {
		return snap, err
	}
	if m.activeLocked() >= m.cfg.MaxActiveCalls {
		return Snapshot{}, ErrTooManyCalls
	}

	id, err := m.newID()
	if err != nil {
		return Snapshot{}, err
	}
	if _, exists := m.calls[id]; exists {
		return Snapshot{}, errors.New("call id collision")
	}

	c := &call{
		id:        id,
		caller:    caller,
		state:     StateRinging,
		createdAt: now,
		deadline:  now.Add(m.cfg.InvitationTimeout),
		invite:    m.signInvite(id, now.Add(m.cfg.InvitationTimeout)),
	}
	if calleeID != "" {
		c.callee = Party{ID: calleeID, Name: calleeID}
	}

	if err := m.attachLocked(c); err != nil {
		return Snapshot{}, err
	}

	snap := c.snapshot()
	m.rememberLocked(caller.ID, "create", requestID, id, snap, now)
	return snap, nil
}

func (m *Manager) Accept(user Party, callID, invite, requestID string) (Snapshot, error) {
	if !user.Valid() {
		return Snapshot{}, ErrUnknownParty
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if snap, ok, err := m.replayLocked(user.ID, "accept", requestID, now); ok {
		return snap, err
	}
	c, err := m.getLocked(callID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := m.expireLocked(c, now); err != nil {
		return Snapshot{}, err
	}
	if c.state == StateTimeout {
		return Snapshot{}, ErrExpired
	}
	if c.state != StateRinging {
		return Snapshot{}, &StateError{From: c.state, To: StateAccepted}
	}

	if user.ID == c.caller.ID {
		return Snapshot{}, ErrSelfCall
	}

	switch c.callee.ID {
	case user.ID:
		if user.Name != "" {
			c.callee.Name = user.Name
		}
	case "":
		if err := m.verifyInvite(invite, c.id, now); err != nil {
			return Snapshot{}, err
		}
		c.callee = user
		m.userCalls[user.ID] = c.id
	default:
		return Snapshot{}, ErrForbidden
	}

	if err := c.transition(StateAccepted, now, m.cfg.ConnectionTimeout); err != nil {
		return Snapshot{}, err
	}
	snap := c.snapshot()
	m.rememberLocked(user.ID, "accept", requestID, c.id, snap, now)
	return snap, nil
}

func (m *Manager) Reject(user Party, callID, invite, reason string) (Snapshot, error) {
	if !user.Valid() {
		return Snapshot{}, ErrUnknownParty
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	c, err := m.getLocked(callID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := m.expireLocked(c, now); err != nil {
		return Snapshot{}, err
	}
	if c.state == StateTimeout {
		return Snapshot{}, ErrExpired
	}
	if c.state != StateRinging {
		return Snapshot{}, &StateError{From: c.state, To: StateRejected}
	}

	if user.ID == c.caller.ID {
		return Snapshot{}, ErrForbidden
	}

	switch c.callee.ID {
	case user.ID:
		if user.Name != "" {
			c.callee.Name = user.Name
		}
	case "":
		if err := m.verifyInvite(invite, c.id, now); err != nil {
			return Snapshot{}, err
		}
		c.callee = user
	default:
		return Snapshot{}, ErrForbidden
	}

	if err := c.transition(StateRejected, now, m.cfg.ConnectionTimeout); err != nil {
		return Snapshot{}, err
	}
	c.reason = reason
	return c.snapshot(), nil
}

func (m *Manager) Cancel(userID, callID, reason string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	c, err := m.lookupLocked(userID, callID)
	if err != nil {
		return Snapshot{}, err
	}
	if c.caller.ID != userID {
		return Snapshot{}, ErrForbidden
	}
	if err := m.expireLocked(c, now); err != nil {
		return Snapshot{}, err
	}
	switch c.state {
	case StateRinging, StateAccepted, StateConnecting:
	default:
		return Snapshot{}, &StateError{From: c.state, To: StateCancelled}
	}
	if err := c.transition(StateCancelled, now, m.cfg.ConnectionTimeout); err != nil {
		return Snapshot{}, err
	}
	c.reason = reason
	return c.snapshot(), nil
}

func (m *Manager) End(userID, callID, reason string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	c, err := m.lookupLocked(userID, callID)
	if err != nil {
		return Snapshot{}, err
	}
	if _, _, ok := c.participant(userID); !ok {
		return Snapshot{}, ErrForbidden
	}
	if c.state == StateEnded {
		return c.snapshot(), nil
	}
	if c.state.Terminal() {
		return Snapshot{}, &StateError{From: c.state, To: StateEnded}
	}
	if err := c.transition(StateEnded, now, m.cfg.ConnectionTimeout); err != nil {
		return Snapshot{}, err
	}
	c.reason = reason
	return c.snapshot(), nil
}

func (m *Manager) Fail(callID, reason string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	c, ok := m.calls[callID]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	if c.state.Terminal() {
		return c.snapshot(), &StateError{From: c.state, To: StateFailed}
	}
	if err := c.transition(StateFailed, now, m.cfg.ConnectionTimeout); err != nil {
		return Snapshot{}, err
	}
	c.reason = reason
	return c.snapshot(), nil
}

func (m *Manager) BeginNegotiation(userID, callID string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	c, err := m.lookupLocked(userID, callID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := m.expireLocked(c, now); err != nil {
		return Snapshot{}, err
	}
	switch c.state {
	case StateAccepted:
		if err := c.transition(StateConnecting, now, m.cfg.ConnectionTimeout); err != nil {
			return Snapshot{}, err
		}
	case StateConnecting, StateConnected:
	default:
		return Snapshot{}, &StateError{From: c.state, To: StateConnecting}
	}
	return c.snapshot(), nil
}

func (m *Manager) ReportReady(userID, callID string, ready bool) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	c, err := m.lookupLocked(userID, callID)
	if err != nil {
		return Snapshot{}, err
	}
	_, role, ok := c.participant(userID)
	if !ok {
		return Snapshot{}, ErrForbidden
	}
	if c.state.Terminal() {
		return Snapshot{}, &StateError{From: c.state}
	}

	if !ready {
		switch role {
		case RoleCaller:
			c.callerReady = false
		case RoleCallee:
			c.calleeReady = false
		}
		if c.state == StateConnected {
			if err := c.transition(StateConnecting, now, m.cfg.ConnectionTimeout); err != nil {
				return Snapshot{}, err
			}
			c.connectedAt = time.Time{}
		}
		return c.snapshot(), nil
	}

	switch role {
	case RoleCaller:
		c.callerReady = true
	case RoleCallee:
		c.calleeReady = true
	}

	if c.state == StateConnected {
		return c.snapshot(), nil
	}
	if c.state != StateAccepted && c.state != StateConnecting {
		return Snapshot{}, &StateError{From: c.state, To: StateConnected}
	}
	if c.callerReady && c.calleeReady {
		if err := c.transition(StateConnected, now, m.cfg.ConnectionTimeout); err != nil {
			return Snapshot{}, err
		}
	}
	return c.snapshot(), nil
}

func (m *Manager) Get(userID, callID string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, err := m.lookupLocked(userID, callID)
	if err != nil {
		return Snapshot{}, err
	}
	return c.snapshot(), nil
}

func (m *Manager) ActiveSnapshots() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Snapshot, 0, len(m.calls))
	for _, c := range m.calls {
		if !c.state.Terminal() {
			out = append(out, c.snapshot())
		}
	}
	return out
}

func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked()
}

func (m *Manager) Sweep() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	for _, c := range m.calls {
		_ = m.expireLocked(c, now)
	}
	for id, rec := range m.idem {
		if now.After(rec.expires) {
			delete(m.idem, id)
		}
	}
	for id, c := range m.calls {
		if c.state.Terminal() && now.Sub(c.terminalAt) > m.cfg.Retention {
			m.detachLocked(c)
			delete(m.calls, id)
		}
	}

	out := make([]Snapshot, 0, len(m.pendingOrder))
	for _, id := range m.pendingOrder {
		snap, ok := m.pending[id]
		if !ok {
			continue
		}
		out = append(out, snap)
		delete(m.pending, id)
	}
	m.pendingOrder = m.pendingOrder[:0]
	return out
}

func (m *Manager) expireLocked(c *call, now time.Time) error {
	if c.state.Terminal() || c.deadline.IsZero() || now.Before(c.deadline) {
		return nil
	}
	target := StateFailed
	if c.state == StateRinging {
		target = StateTimeout
	}
	c.reason = string(target)
	if err := c.transition(target, now, m.cfg.ConnectionTimeout); err != nil {
		return err
	}
	m.enqueueLocked(c.snapshot())
	return ErrExpired
}

func (m *Manager) enqueueLocked(snap Snapshot) {
	if _, ok := m.pending[snap.ID]; !ok {
		m.pendingOrder = append(m.pendingOrder, snap.ID)
	}
	m.pending[snap.ID] = snap
}

func (m *Manager) getLocked(callID string) (*call, error) {
	c, ok := m.calls[callID]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (m *Manager) lookupLocked(userID, callID string) (*call, error) {
	c, ok := m.calls[callID]
	if !ok {
		return nil, ErrNotFound
	}
	if _, _, ok := c.participant(userID); !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (m *Manager) activeLocked() int {
	n := 0
	for _, c := range m.calls {
		if !c.state.Terminal() {
			n++
		}
	}
	return n
}

func (m *Manager) attachLocked(c *call) error {
	m.calls[c.id] = c
	if err := m.claimLocked(c.caller.ID, c.id); err != nil {
		delete(m.calls, c.id)
		return err
	}
	if c.callee.ID != "" {
		if err := m.claimLocked(c.callee.ID, c.id); err != nil {
			delete(m.calls, c.id)
			delete(m.userCalls, c.caller.ID)
			return err
		}
	}
	return nil
}

func (m *Manager) claimLocked(userID, callID string) error {
	if userID == "" {
		return nil
	}
	if existing, ok := m.userCalls[userID]; ok && existing != callID {
		if c, ok := m.calls[existing]; ok && !c.state.Terminal() {
			return ErrAlreadyInCall
		}
	}
	m.userCalls[userID] = callID
	return nil
}

func (m *Manager) detachLocked(c *call) {
	for _, member := range c.snapshot().Members() {
		if m.userCalls[member] == c.id {
			delete(m.userCalls, member)
		}
	}
}

func (m *Manager) idemKey(userID, requestID string) string {
	return userID + "\x00" + requestID
}

func (m *Manager) replayLocked(userID, op, requestID string, now time.Time) (Snapshot, bool, error) {
	if requestID == "" {
		return Snapshot{}, false, nil
	}
	rec, ok := m.idem[m.idemKey(userID, requestID)]
	if !ok || now.After(rec.expires) {
		return Snapshot{}, false, nil
	}
	if rec.op != op {
		return Snapshot{}, true, ErrRequestReuse
	}
	if c, ok := m.calls[rec.callID]; ok {
		return c.snapshot(), true, nil
	}
	return rec.result, true, nil
}

func (m *Manager) rememberLocked(userID, op, requestID, callID string, snap Snapshot, now time.Time) {
	if requestID == "" {
		return
	}
	m.idem[m.idemKey(userID, requestID)] = idemRecord{
		userID:  userID,
		op:      op,
		callID:  callID,
		result:  snap,
		expires: now.Add(m.cfg.IdempotencyTTL),
	}
}

func randomCallID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (m *Manager) signInvite(callID string, exp time.Time) string {
	payload := callID + "|" + strconv.FormatInt(exp.Unix(), 10)
	return encodeInvite(payload, m.inviteKey(payload))
}

func (m *Manager) verifyInvite(token, callID string, now time.Time) error {
	payload, ok := decodeInvite(token)
	if !ok {
		return ErrInviteInvalid
	}
	expected := m.inviteKey(payload)
	provided, ok := inviteMAC(token)
	if !ok || !hmac.Equal(expected, provided) {
		return ErrInviteInvalid
	}
	id, expText, ok := strings.Cut(payload, "|")
	if !ok || id != callID {
		return ErrInviteInvalid
	}
	exp, err := strconv.ParseInt(expText, 10, 64)
	if err != nil {
		return ErrInviteInvalid
	}
	if now.After(time.Unix(exp, 0)) {
		return ErrExpired
	}
	return nil
}

func (m *Manager) inviteKey(payload string) []byte {
	mac := hmac.New(sha256.New, m.inviteSecret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func encodeInvite(payload string, mac []byte) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac)
}

func decodeInvite(token string) (string, bool) {
	body, _, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func inviteMAC(token string) ([]byte, bool) {
	_, tail, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(tail)
	if err != nil {
		return nil, false
	}
	return raw, true
}
