package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/time/rate"

	"github.com/Alice088/worm/internal/auth"
	"github.com/Alice088/worm/internal/call"
	"github.com/Alice088/worm/internal/protocol"
)

type session struct {
	srv  *Server
	conn *websocket.Conn
	id   auth.Identity

	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once

	msgLimiter    *rate.Limiter
	createLimiter *rate.Limiter

	seenMu sync.Mutex
	seen   map[string]time.Time

	statsMu      sync.Mutex
	countedConn  bool
	reportedFail bool
	restartCount int
}

func newSession(srv *Server, conn *websocket.Conn, id auth.Identity) *session {
	return &session{
		srv:           srv,
		conn:          conn,
		id:            id,
		out:           make(chan []byte, srv.cfg.SendQueue),
		done:          make(chan struct{}),
		msgLimiter:    rate.NewLimiter(rate.Limit(srv.cfg.MessageRate), srv.cfg.MessageBurst),
		createLimiter: rate.NewLimiter(rate.Limit(srv.cfg.CreateRate), srv.cfg.CreateBurst),
		seen:          make(map[string]time.Time),
	}
}

func (s *session) writeLoop() {
	ping := s.srv.cfg.IdleTimeout / 3
	if ping < 5*time.Second {
		ping = 5 * time.Second
	}
	ticker := time.NewTicker(ping)
	defer ticker.Stop()

	for {
		select {
		case data := <-s.out:
			if !s.write(data) {
				return
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), s.srv.cfg.WriteTimeout)
			err := s.conn.Ping(ctx)
			cancel()
			if err != nil {
				s.shutdown("ping failed")
				return
			}
		case <-s.done:
			return
		}
	}
}

func (s *session) write(data []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), s.srv.cfg.WriteTimeout)
	defer cancel()

	if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
		s.shutdown("write failed")
		return false
	}
	return true
}

func (s *session) readLoop(parent context.Context) {
	for {
		ctx, cancel := context.WithTimeout(parent, s.srv.cfg.IdleTimeout)
		kind, data, err := s.conn.Read(ctx)
		cancel()
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			s.failEnvelope(protocol.Envelope{}, protocol.CodeInvalidMessage, "text frames are required")
			continue
		}
		s.handleMessage(data)
	}
}

func (s *session) handleMessage(data []byte) {
	env, err := protocol.Decode(data)
	if err != nil {
		s.failEnvelope(env, protocol.AsError(err).Code, err.Error())
		return
	}
	s.srv.cfg.Metrics.SignalingMessages.WithLabelValues(string(env.Type)).Inc()

	if !s.msgLimiter.Allow() {
		s.failEnvelope(env, protocol.CodeRateLimited, "signaling rate limit exceeded")
		return
	}

	switch env.Type {
	case protocol.TypePing:
		s.handlePing(env)
	case protocol.TypeCallCreate:
		s.handleCreate(env)
	case protocol.TypeCallAccept:
		s.handleAccept(env)
	case protocol.TypeCallReject:
		s.handleReject(env)
	case protocol.TypeCallCancel:
		s.handleCancel(env)
	case protocol.TypeCallEnd:
		s.handleEnd(env)
	case protocol.TypeWebRTCOffer, protocol.TypeWebRTCAnswer:
		s.handleSDP(env)
	case protocol.TypeWebRTCICECandidate:
		s.handleICECandidate(env)
	case protocol.TypeWebRTCICEComplete:
		s.handleICEComplete(env)
	case protocol.TypeWebRTCRenegotiate:
		s.handleRenegotiate(env)
	case protocol.TypeConnectionStatus:
		s.handleStatus(env)
	case protocol.TypeConnectionRecover:
		s.handleRecover(env)
	default:
		s.failEnvelope(env, protocol.CodeInvalidMessage, "message type is not accepted from clients")
	}
}

func (s *session) handlePing(env protocol.Envelope) {
	var p protocol.PingPayload
	if len(env.Payload) > 0 {
		if err := env.DecodePayload(&p); err != nil {
			s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
			return
		}
	}
	raw, _ := marshalPayload(p)
	s.send(protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypePong,
		RequestID: env.RequestID,
		Payload:   raw,
	})
}

func (s *session) handleCreate(env protocol.Envelope) {
	var p protocol.CallCreatePayload
	if len(env.Payload) > 0 {
		if err := env.DecodePayload(&p); err != nil {
			s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
			return
		}
	}
	if !s.createLimiter.Allow() {
		s.srv.cfg.Metrics.ObserveCallRejected("rate_limited")
		s.failEnvelope(env, protocol.CodeRateLimited, "call creation rate limit exceeded")
		return
	}

	party := call.Party{ID: s.id.ID, Name: s.id.Name}
	snap, err := s.srv.cfg.Manager.Create(party, p.CalleeID, env.RequestID)
	if err != nil {
		s.srv.cfg.Metrics.ObserveCallRejected("error")
		s.failFromError(env, err)
		return
	}
	s.srv.cfg.Metrics.ObserveCallCreated()

	if snap.Callee.ID != "" {
		if !s.srv.online(snap.Callee.ID) {
			if cancelled, cerr := s.srv.cfg.Manager.Cancel(s.id.ID, snap.ID, "callee_unavailable"); cerr == nil {
				s.srv.broadcastState(cancelled)
			}
			s.srv.cfg.Metrics.ObserveCallRejected("callee_unavailable")
			s.failEnvelope(env, protocol.CodeUnavailable, "callee is not connected")
			return
		}
		if incoming, err := s.srv.incomingEnvelope(snap, snap.Callee.ID); err == nil {
			s.srv.send(snap.Callee.ID, incoming)
		}
	}

	s.sendState(env, snap)
}

func (s *session) handleAccept(env protocol.Envelope) {
	var p protocol.CallAcceptPayload
	if len(env.Payload) > 0 {
		if err := env.DecodePayload(&p); err != nil {
			s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
			return
		}
	}
	party := call.Party{ID: s.id.ID, Name: s.id.Name}
	snap, err := s.srv.cfg.Manager.Accept(party, env.CallID, p.Invite, env.RequestID)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.srv.cfg.Metrics.ObserveCallRejected("accepted")
	if incoming, err := s.srv.incomingEnvelope(snap, s.id.ID); err == nil {
		s.send(incoming)
	}
	s.srv.broadcastState(snap)
}

func (s *session) handleReject(env protocol.Envelope) {
	var p protocol.CallRejectPayload
	if len(env.Payload) > 0 {
		if err := env.DecodePayload(&p); err != nil {
			s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
			return
		}
	}
	reason, err := protocol.ValidateReason(p.Reason)
	if err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}
	party := call.Party{ID: s.id.ID, Name: s.id.Name}
	snap, err := s.srv.cfg.Manager.Reject(party, env.CallID, p.Invite, reason)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.srv.cfg.Metrics.ObserveCallRejected("rejected")
	s.srv.recordTerminal(snap)
	s.srv.broadcastState(snap)
}

func (s *session) handleCancel(env protocol.Envelope) {
	var p protocol.CallEndPayload
	if len(env.Payload) > 0 {
		if err := env.DecodePayload(&p); err != nil {
			s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
			return
		}
	}
	reason, err := protocol.ValidateReason(p.Reason)
	if err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}
	snap, err := s.srv.cfg.Manager.Cancel(s.id.ID, env.CallID, reason)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.srv.cfg.Metrics.ObserveCallRejected("cancelled")
	s.srv.recordTerminal(snap)
	s.srv.broadcastState(snap)
}

func (s *session) handleEnd(env protocol.Envelope) {
	var p protocol.CallEndPayload
	if len(env.Payload) > 0 {
		if err := env.DecodePayload(&p); err != nil {
			s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
			return
		}
	}
	reason, err := protocol.ValidateReason(p.Reason)
	if err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}
	snap, err := s.srv.cfg.Manager.End(s.id.ID, env.CallID, reason)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.srv.recordTerminal(snap)
	s.srv.broadcastState(snap)
}

func (s *session) handleSDP(env protocol.Envelope) {
	var p protocol.SDPPayload
	if err := env.DecodePayload(&p); err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		s.failEnvelope(env, protocol.AsError(err).Code, err.Error())
		return
	}
	if !s.markSeen(env.RequestID) {
		return
	}
	peer, err := s.negotiated(env)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.forward(peer, env)
}

func (s *session) handleICECandidate(env protocol.Envelope) {
	var p protocol.ICECandidatePayload
	if err := env.DecodePayload(&p); err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		s.failEnvelope(env, protocol.AsError(err).Code, err.Error())
		return
	}
	peer, err := s.negotiated(env)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.forward(peer, env)
}

func (s *session) handleICEComplete(env protocol.Envelope) {
	peer, err := s.negotiated(env)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.forward(peer, env)
}

func (s *session) handleRenegotiate(env protocol.Envelope) {
	peer, err := s.negotiated(env)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.forward(peer, env)
}

func (s *session) negotiated(env protocol.Envelope) (string, error) {
	snap, err := s.srv.cfg.Manager.Get(s.id.ID, env.CallID)
	if err != nil {
		return "", err
	}
	switch snap.State {
	case call.StateAccepted, call.StateConnecting, call.StateConnected:
	default:
		return "", &call.StateError{From: snap.State, To: call.StateConnecting}
	}
	peer, ok := snap.Peer(s.id.ID)
	if !ok || peer.ID == "" {
		return "", call.ErrNotFound
	}
	updated, err := s.srv.cfg.Manager.BeginNegotiation(s.id.ID, env.CallID)
	if err != nil {
		return "", err
	}
	if updated.State != snap.State {
		s.srv.broadcastState(updated)
	}
	return peer.ID, nil
}

func (s *session) handleStatus(env protocol.Envelope) {
	var p protocol.ConnectionStatusPayload
	if err := env.DecodePayload(&p); err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}
	if err := validateStatus(p); err != nil {
		s.failEnvelope(env, protocol.CodeInvalidMessage, err.Error())
		return
	}

	privileged := p.RTTMillis >= 0 && p.RTTMillis < 120_000 &&
		p.JitterMillis >= 0 && p.JitterMillis < 120_000 &&
		p.PacketsLost >= 0 && p.PacketsRecv >= 0
	if !privileged {
		s.failEnvelope(env, protocol.CodeInvalidMessage, "connection metrics are out of range")
		return
	}

	ready := p.PCState == "connected" && (p.ICEState == "connected" || p.ICEState == "completed") && p.MediaState == "flowing"
	degraded := p.PCState == "failed" || p.PCState == "closed" || p.PCState == "disconnected" ||
		p.ICEState == "failed" || p.ICEState == "closed" || p.ICEState == "disconnected" ||
		p.MediaState == "stalled" || p.MediaState == "unavailable"

	if !ready && !degraded {
		return
	}

	snap, err := s.srv.cfg.Manager.ReportReady(s.id.ID, env.CallID, ready)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.observeStatus(p, snap)
	if snap.State == call.StateConnected || snap.State == call.StateConnecting {
		s.srv.broadcastState(snap)
	}
}

func (s *session) observeStatus(p protocol.ConnectionStatusPayload, snap call.Snapshot) {
	m := s.srv.cfg.Metrics
	if p.RTTMillis > 0 {
		m.MediaRTT.Observe(p.RTTMillis / 1000)
	}
	if p.JitterMillis > 0 {
		m.MediaJitter.Observe(p.JitterMillis / 1000)
	}
	if p.PacketsLost > 0 {
		m.MediaPacketsLost.Add(float64(p.PacketsLost))
	}

	s.statsMu.Lock()
	if p.RestartCount > s.restartCount {
		m.ICERestarts.Add(float64(p.RestartCount - s.restartCount))
		s.restartCount = p.RestartCount
	}
	if p.ICEState == "failed" && !s.reportedFail {
		m.ICEFailures.Inc()
		s.reportedFail = true
	}
	firstConnected := false
	if snap.State == call.StateConnected && !s.countedConn {
		s.countedConn = true
		firstConnected = true
	}
	s.statsMu.Unlock()

	if firstConnected {
		if p.Relay {
			m.RelayConnections.Inc()
		} else {
			m.DirectConnections.Inc()
		}
	}
}

func (s *session) handleRecover(env protocol.Envelope) {
	snap, err := s.srv.cfg.Manager.Get(s.id.ID, env.CallID)
	if err != nil {
		s.failFromError(env, err)
		return
	}
	s.sendState(env, snap)
	s.srv.flushPending(s)
}

func (s *session) forward(peerID string, env protocol.Envelope) {
	env.RequestID = ""
	s.srv.sendOrBuffer(peerID, env)
}

func (s *session) sendState(env protocol.Envelope, snap call.Snapshot) {
	out, err := s.srv.stateEnvelope(snap, s.id.ID)
	if err != nil {
		s.failEnvelope(env, protocol.CodeInternal, "cannot encode state")
		return
	}
	s.send(out)
}

func (s *session) send(env protocol.Envelope) bool {
	data, err := env.Encode()
	if err != nil {
		return false
	}
	select {
	case s.out <- data:
		return true
	case <-s.done:
		return false
	default:
		s.srv.cfg.Metrics.SignalingDropped.Inc()
		s.shutdown("outbound queue overflow")
		return false
	}
}

func (s *session) failFromError(env protocol.Envelope, err error) {
	code := mapError(err)
	if code == protocol.CodeInternal {
		s.srv.cfg.Logger.Warn("signaling internal error",
			"user_id", s.id.ID, "call_id", env.CallID, "type", string(env.Type), "error", err.Error())
	}
	s.failEnvelope(env, code, err.Error())
}

func (s *session) failEnvelope(env protocol.Envelope, code protocol.ErrorCode, message string) {
	s.srv.cfg.Metrics.SignalingErrors.WithLabelValues(string(code)).Inc()
	raw, err := marshalPayload(protocol.ErrorPayload{
		Code:      code,
		Message:   message,
		RequestID: env.RequestID,
	})
	if err != nil {
		return
	}
	s.send(protocol.Envelope{
		Version: protocol.Version,
		Type:    protocol.TypeCallError,
		CallID:  env.CallID,
		Payload: raw,
	})
}

func (s *session) markSeen(requestID string) bool {
	if requestID == "" {
		return true
	}
	now := time.Now()
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if _, ok := s.seen[requestID]; ok {
		return false
	}
	for id, at := range s.seen {
		if now.Sub(at) > 2*time.Minute {
			delete(s.seen, id)
		}
	}
	s.seen[requestID] = now
	return true
}

func (s *session) shutdown(reason string) {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.conn.Close(websocket.StatusGoingAway, reason)
	})
}

func marshalPayload(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func mapError(err error) protocol.ErrorCode {
	var stateErr *call.StateError
	var protoErr *protocol.Error
	switch {
	case errors.As(err, &protoErr):
		return protoErr.Code
	case errors.As(err, &stateErr):
		return protocol.CodeInvalidState
	case errors.Is(err, call.ErrNotFound):
		return protocol.CodeNotFound
	case errors.Is(err, call.ErrForbidden):
		return protocol.CodeForbidden
	case errors.Is(err, call.ErrInviteInvalid):
		return protocol.CodeForbidden
	case errors.Is(err, call.ErrExpired):
		return protocol.CodeExpired
	case errors.Is(err, call.ErrAlreadyInCall):
		return protocol.CodeAlreadyInCall
	case errors.Is(err, call.ErrTooManyCalls):
		return protocol.CodeRateLimited
	case errors.Is(err, call.ErrRequestReuse):
		return protocol.CodeRequestConflict
	case errors.Is(err, call.ErrSelfCall):
		return protocol.CodeBadRequest
	default:
		return protocol.CodeInternal
	}
}

func validateStatus(p protocol.ConnectionStatusPayload) error {
	valid := func(v string, allowed ...string) bool {
		for _, a := range allowed {
			if v == a {
				return true
			}
		}
		return false
	}
	if !valid(p.PCState, "new", "connecting", "connected", "disconnected", "failed", "closed") {
		return errors.New("invalid pc_state")
	}
	if !valid(p.ICEState, "new", "checking", "connected", "completed", "disconnected", "failed", "closed") {
		return errors.New("invalid ice_state")
	}
	if !valid(p.MediaState, "inactive", "starting", "flowing", "stalled", "unavailable") {
		return errors.New("invalid media_state")
	}
	if p.RestartCount < 0 || p.RestartCount > 1000 || p.ReconnectTries < 0 || p.ReconnectTries > 1000 {
		return errors.New("invalid retry counters")
	}
	return nil
}
