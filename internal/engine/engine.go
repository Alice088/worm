package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/Alice088/worm/internal/audio"
	"github.com/Alice088/worm/internal/client"
	"github.com/Alice088/worm/internal/protocol"
	"github.com/Alice088/worm/internal/session"
)

type EventKind int

const (
	EventState EventKind = iota
	EventNotice
	EventError
	EventStats
	EventMedia
)

type Event struct {
	Kind      EventKind
	Call      protocol.CallStatePayload
	Code      protocol.ErrorCode
	Message   string
	Connected bool
}

type Config struct {
	Name          string
	ServerURL     string
	Token         string
	ICEServers    []protocol.ICEServer
	Audio         audio.Opener
	Logger        *slog.Logger
	Signaling     client.SignalingConfig
	StatsInterval time.Duration
	DialTimeout   time.Duration
	Tune          func(*webrtc.SettingEngine)
}

func (c *Config) withDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Audio.Source == nil && c.Audio.Sink == nil {
		c.Audio = audio.Default
	}
	if c.StatsInterval <= 0 {
		c.StatsInterval = 5 * time.Second
	}
	if c.Signaling.URL == "" {
		c.Signaling.URL = c.ServerURL
	}
	if c.Signaling.Token == "" {
		c.Signaling.Token = c.Token
	}
	if c.Signaling.Logger == nil {
		c.Signaling.Logger = c.Logger
	}
}

type Engine struct {
	cfg    Config
	events chan Event

	cmds chan any
	done chan struct{}

	closeOnce sync.Once
	wg        sync.WaitGroup
}

func New(cfg Config) *Engine {
	cfg.withDefaults()
	e := &Engine{
		cfg:    cfg,
		events: make(chan Event, 64),
		cmds:   make(chan any, 16),
		done:   make(chan struct{}),
	}
	e.wg.Add(1)
	go e.run()
	return e
}

func (e *Engine) Events() <-chan Event { return e.events }

func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		close(e.done)
	})
	e.wg.Wait()
	return nil
}

func (e *Engine) Connect(ctx context.Context) error {
	return e.request(ctx, func(resp chan error) any { return cmdConnect{resp: resp} })
}

func (e *Engine) CreateCall(ctx context.Context) error {
	return e.request(ctx, func(resp chan error) any { return cmdCreate{resp: resp} })
}

func (e *Engine) Accept(ctx context.Context, callID, invite string) error {
	return e.request(ctx, func(resp chan error) any {
		return cmdAccept{callID: callID, invite: invite, resp: resp}
	})
}

func (e *Engine) Reject(ctx context.Context, callID, invite, reason string) error {
	return e.request(ctx, func(resp chan error) any {
		return cmdReject{callID: callID, invite: invite, reason: reason, resp: resp}
	})
}

func (e *Engine) Cancel(ctx context.Context, callID string) error {
	return e.request(ctx, func(resp chan error) any { return cmdCancel{callID: callID, resp: resp} })
}

func (e *Engine) End(ctx context.Context, callID, reason string) error {
	return e.request(ctx, func(resp chan error) any {
		return cmdEnd{callID: callID, reason: reason, resp: resp}
	})
}

func (e *Engine) SetMuted(muted bool) {
	select {
	case e.cmds <- cmdMute{muted: muted}:
	case <-e.done:
	default:
	}
}

func (e *Engine) request(ctx context.Context, build func(chan error) any) error {
	resp := make(chan error, 1)
	select {
	case e.cmds <- build(resp):
	case <-e.done:
		return errors.New("engine is closed")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-resp:
		return err
	case <-e.done:
		return errors.New("engine is closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

type cmdConnect struct{ resp chan error }
type cmdCreate struct{ resp chan error }
type cmdAccept struct {
	callID string
	invite string
	resp   chan error
}
type cmdReject struct {
	callID string
	invite string
	reason string
	resp   chan error
}
type cmdCancel struct {
	callID string
	resp   chan error
}
type cmdEnd struct {
	callID string
	reason string
	resp   chan error
}
type cmdMute struct{ muted bool }

type peerEvent struct {
	kind      string
	candidate webrtcCandidate
	offer     string
	pcState   string
	iceState  string
	stats     statsSnapshot
	restarts  int
	err       error
}

type webrtcCandidate struct {
	Candidate        string
	SDPMid           *string
	SDPMLineIndex    *uint16
	UsernameFragment *string
}

func (e *Engine) run() {
	defer e.wg.Done()

	a := &actor{
		cfg:       e.cfg,
		events:    e.events,
		peerIn:    make(chan peerEvent, 256),
		done:      e.done,
		requestID: func() string { return session.RandomID(8) },
	}

	for {
		var (
			in     <-chan protocol.Envelope
			status <-chan client.ConnectionState
		)
		if a.sig != nil {
			in = a.sig.In()
			status = a.sig.Status()
		}

		select {
		case <-e.done:
			a.close()
			close(e.events)
			return
		case cmd := <-e.cmds:
			a.handleCommand(cmd)
		case env, ok := <-in:
			if !ok {
				a.sig = nil
				continue
			}
			a.handleEnvelope(env)
		case st, ok := <-status:
			if !ok {
				continue
			}
			a.handleStatus(st)
		case ev := <-a.peerIn:
			a.handlePeerEvent(ev)
		}
	}
}

type actor struct {
	cfg    Config
	events chan Event
	peerIn chan peerEvent
	done   <-chan struct{}

	sig  *client.Signaling
	peer *peerSession

	call     protocol.CallStatePayload
	callID   string
	invite   string
	muted    bool
	audioErr error
	ended    bool

	requestID func() string
}

func (a *actor) emit(event Event) {
	select {
	case a.events <- event:
	case <-a.done:
	}
}

func (a *actor) emitCallState(call protocol.CallStatePayload) {
	a.emit(Event{Kind: EventState, Call: call})
}

func (a *actor) notice(format string, args ...any) {
	a.emit(Event{Kind: EventNotice, Message: fmt.Sprintf(format, args...)})
}

func (a *actor) emitError(code protocol.ErrorCode, message string) {
	a.emit(Event{Kind: EventError, Code: code, Message: message})
}

func (a *actor) handleCommand(cmd any) {
	switch c := cmd.(type) {
	case cmdConnect:
		a.connect(c.resp)
	case cmdCreate:
		a.createCall(c.resp)
	case cmdAccept:
		a.acceptCall(c)
	case cmdReject:
		a.rejectCall(c)
	case cmdCancel:
		a.cancelCall(c)
	case cmdEnd:
		a.endCall(c)
	case cmdMute:
		a.setMuted(c.muted)
	}
}

func (a *actor) connect(resp chan error) {
	if a.sig != nil {
		resp <- nil
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.dialTimeout())
	defer cancel()

	sig, err := client.Dial(ctx, a.cfg.Signaling)
	if err != nil {
		resp <- err
		return
	}
	a.sig = sig
	a.notice("connected to signaling server")
	resp <- nil
}

func (a *actor) dialTimeout() time.Duration {
	if a.cfg.DialTimeout > 0 {
		return a.cfg.DialTimeout
	}
	if a.cfg.Signaling.DialTimeout > 0 {
		return a.cfg.Signaling.DialTimeout
	}
	return 10 * time.Second
}

func (a *actor) createCall(resp chan error) {
	if err := a.ready(); err != nil {
		resp <- err
		return
	}
	raw, _ := marshal(protocol.CallCreatePayload{})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeCallCreate,
		RequestID: a.requestID(),
		Payload:   raw,
	}
	if err := a.sig.Send(env); err != nil {
		resp <- err
		return
	}
	resp <- nil
}

func (a *actor) acceptCall(c cmdAccept) {
	if err := a.ready(); err != nil {
		c.resp <- err
		return
	}
	a.callID = c.callID
	a.invite = c.invite
	raw, _ := marshal(protocol.CallAcceptPayload{Invite: c.invite})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeCallAccept,
		RequestID: a.requestID(),
		CallID:    c.callID,
		Payload:   raw,
	}
	if err := a.sig.Send(env); err != nil {
		c.resp <- err
		return
	}
	c.resp <- nil
}

func (a *actor) rejectCall(c cmdReject) {
	if err := a.ready(); err != nil {
		c.resp <- err
		return
	}
	raw, _ := marshal(protocol.CallRejectPayload{Invite: c.invite, Reason: c.reason})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeCallReject,
		RequestID: a.requestID(),
		CallID:    c.callID,
		Payload:   raw,
	}
	c.resp <- a.sig.Send(env)
}

func (a *actor) cancelCall(c cmdCancel) {
	if err := a.ready(); err != nil {
		c.resp <- err
		return
	}
	raw, _ := marshal(protocol.CallEndPayload{Reason: "cancelled"})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeCallCancel,
		RequestID: a.requestID(),
		CallID:    c.callID,
		Payload:   raw,
	}
	a.ended = true
	c.resp <- a.sig.Send(env)
}

func (a *actor) endCall(c cmdEnd) {
	if err := a.ready(); err != nil {
		c.resp <- err
		return
	}
	reason := c.reason
	if reason == "" {
		reason = "ended"
	}
	raw, _ := marshal(protocol.CallEndPayload{Reason: reason})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeCallEnd,
		RequestID: a.requestID(),
		CallID:    c.callID,
		Payload:   raw,
	}
	a.ended = true
	c.resp <- a.sig.Send(env)
}

func (a *actor) setMuted(muted bool) {
	a.muted = muted
	if a.peer != nil {
		a.peer.setMuted(muted)
	}
	if muted {
		a.notice("microphone muted")
	} else {
		a.notice("microphone live")
	}
	a.reportStatus()
}

func (a *actor) ready() error {
	if a.sig == nil {
		return errors.New("not connected to signaling server")
	}
	return nil
}

func (a *actor) handleStatus(st client.ConnectionState) {
	if st.Fatal {
		a.emitError(protocol.CodeUnavailable, "signaling connection lost: "+errString(st.Err))
		return
	}
	if st.Connected {
		if st.Attempt > 0 {
			a.notice("signaling reconnected")
			if a.callID != "" {
				a.recoverCall()
			}
		}
		return
	}
	a.notice("signaling disconnected, reconnecting (attempt %d)", st.Attempt)
}

func (a *actor) recoverCall() {
	raw, _ := marshal(protocol.RecoverPayload{LastState: a.call.State})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeConnectionRecover,
		RequestID: a.requestID(),
		CallID:    a.callID,
		Payload:   raw,
	}
	if err := a.sig.Send(env); err != nil {
		a.notice("cannot recover call: %v", err)
	}
}

func (a *actor) handleEnvelope(env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeCallState, protocol.TypeCallIncoming:
		var payload protocol.CallStatePayload
		if err := env.DecodePayload(&payload); err != nil {
			a.emitError(protocol.AsError(err).Code, err.Error())
			return
		}
		a.applyCallState(payload)
	case protocol.TypeCallError:
		var payload protocol.ErrorPayload
		if err := env.DecodePayload(&payload); err != nil {
			a.emitError(protocol.CodeInvalidMessage, err.Error())
			return
		}
		a.emitError(payload.Code, payload.Message)
	case protocol.TypeWebRTCOffer:
		a.cfg.Logger.Debug("RECV OFFER")
		var payload protocol.SDPPayload
		if err := env.DecodePayload(&payload); err != nil {
			a.emitError(protocol.CodeInvalidMessage, err.Error())
			return
		}
		a.handleOffer(payload.SDP)
	case protocol.TypeWebRTCAnswer:
		a.cfg.Logger.Debug("RECV ANSWER")
		var payload protocol.SDPPayload
		if err := env.DecodePayload(&payload); err != nil {
			a.emitError(protocol.CodeInvalidMessage, err.Error())
			return
		}
		if a.peer == nil {
			a.emitError(protocol.CodeInvalidState, "answer arrived without a peer connection")
			return
		}
		if err := a.peer.acceptAnswer(payload.SDP); err != nil {
			a.emitError(protocol.CodeInternal, "apply answer: "+err.Error())
		}
		a.reportStatus()
	case protocol.TypeWebRTCICECandidate:
		var payload protocol.ICECandidatePayload
		if err := env.DecodePayload(&payload); err != nil {
			a.emitError(protocol.CodeInvalidMessage, err.Error())
			return
		}
		if a.peer == nil {
			return
		}
		init := webrtcCandidateInit(payload)
		a.peer.addCandidate(init)
	case protocol.TypeWebRTCICEComplete:
		if a.peer != nil {
			a.peer.endOfCandidates()
		}
	case protocol.TypeWebRTCRenegotiate:
		a.notice("peer requested renegotiation")
	}
}

func (a *actor) applyCallState(call protocol.CallStatePayload) {
	previous := a.call.State
	a.call = call
	a.callID = call.CallID
	if call.Invite != "" {
		a.invite = call.Invite
	}
	if call.State == "ringing" && previous == "" {
		a.ended = false
		a.audioErr = nil
	}
	a.emitCallState(call)

	switch call.State {
	case "accepted":
		a.ensurePeer()
	case "ended", "rejected", "cancelled", "timeout", "failed":
		a.closePeer()
	}
}

func (a *actor) ensurePeer() {
	if a.peer != nil || a.audioErr != nil {
		return
	}
	peer, err := newPeer(peerConfig{
		CallID:      a.callID,
		ICEServers:  toICEServers(a.cfg.ICEServers),
		Audio:       a.cfg.Audio,
		Logger:      a.cfg.Logger,
		Stats:       a.cfg.StatsInterval,
		Tune:        a.cfg.Tune,
		OnCandidate: func(c webrtcCandidate) { a.pushPeer(peerEvent{kind: "candidate", candidate: c}) },
		OnGatherEnd: func() { a.pushPeer(peerEvent{kind: "gather_end"}) },
		OnState: func(pc, ice string) {
			a.pushPeer(peerEvent{kind: "state", pcState: pc, iceState: ice})
		},
		OnRestart: func(offer string) error {
			a.pushPeer(peerEvent{kind: "restart", offer: offer})
			return nil
		},
		OnStats: func(stats statsSnapshot, restarts int) {
			a.pushPeer(peerEvent{kind: "stats", stats: stats, restarts: restarts})
		},
		OnAudioError: func(err error) {
			a.pushPeer(peerEvent{kind: "audio_error", err: err})
		},
	})
	if err != nil {
		a.audioErr = err
		a.emitError(protocol.CodeInternal, "media setup failed: "+err.Error())
		a.endWithReason("media_setup_failed")
		return
	}
	a.peer = peer
	a.peer.setMuted(a.muted)
	a.reportStatus()

	if a.call.Role == "caller" {
		offer, err := peer.createOffer(false)
		if err != nil {
			a.emitError(protocol.CodeInternal, "create offer: "+err.Error())
			a.endWithReason("sdp_offer_failed")
			return
		}
		a.sendOffer(offer, false)
	}
}

func (a *actor) handleOffer(sdp string) {
	if a.peer == nil {
		a.ensurePeer()
	}
	if a.peer == nil {
		return
	}
	answer, err := a.peer.acceptOffer(sdp)
	if err != nil {
		a.emitError(protocol.CodeInternal, "apply offer: "+err.Error())
		a.endWithReason("sdp_answer_failed")
		return
	}
	raw, _ := marshal(protocol.SDPPayload{SDP: answer})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeWebRTCAnswer,
		RequestID: a.requestID(),
		CallID:    a.callID,
		Payload:   raw,
	}
	a.cfg.Logger.Debug("SEND ANSWER", "call", a.callID)
	if err := a.sig.Send(env); err != nil {
		a.notice("cannot send answer: %v", err)
	}
	a.reportStatus()
}

func (a *actor) sendOffer(sdp string, renegotiate bool) {
	if renegotiate {
		raw, _ := marshal(struct{}{})
		_ = a.sig.Send(protocol.Envelope{
			Version:   protocol.Version,
			Type:      protocol.TypeWebRTCRenegotiate,
			RequestID: a.requestID(),
			CallID:    a.callID,
			Payload:   raw,
		})
	}
	raw, _ := marshal(protocol.SDPPayload{SDP: sdp})
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeWebRTCOffer,
		RequestID: a.requestID(),
		CallID:    a.callID,
		Payload:   raw,
	}
	a.cfg.Logger.Debug("SEND OFFER", "call", a.callID, "renog", renegotiate)
	if err := a.sig.Send(env); err != nil {
		a.notice("cannot send offer: %v", err)
	}
}

func (a *actor) handlePeerEvent(ev peerEvent) {
	switch ev.kind {
	case "candidate":
		a.cfg.Logger.Debug("SEND CAND", "cand", ev.candidate.Candidate)
		payload := protocol.ICECandidatePayload{
			Candidate:        ev.candidate.Candidate,
			SDPMid:           ev.candidate.SDPMid,
			SDPMLineIndex:    ev.candidate.SDPMLineIndex,
			UsernameFragment: ev.candidate.UsernameFragment,
		}
		raw, _ := marshal(payload)
		env := protocol.Envelope{
			Version:   protocol.Version,
			Type:      protocol.TypeWebRTCICECandidate,
			RequestID: a.requestID(),
			CallID:    a.callID,
			Payload:   raw,
		}
		if err := a.sig.Send(env); err != nil {
			a.notice("cannot send ice candidate: %v", err)
		}
	case "gather_end":
		raw, _ := marshal(struct{}{})
		_ = a.sig.Send(protocol.Envelope{
			Version:   protocol.Version,
			Type:      protocol.TypeWebRTCICEComplete,
			RequestID: a.requestID(),
			CallID:    a.callID,
			Payload:   raw,
		})
	case "state":
		a.reportStatus()
		if ev.pcState == "failed" {
			a.notice("media connection failed")
		}
	case "restart":
		a.sendOffer(ev.offer, true)
	case "stats":
		a.reportStatus()
		if a.call.State == "connected" {
			route := "direct"
			if ev.stats.Relay {
				route = "relayed via turn"
			}
			a.emit(Event{
				Kind: EventStats,
				Message: fmt.Sprintf("rtt %.0fms · jitter %.0fms · loss %d · %s",
					ev.stats.RTT*1000, ev.stats.Jitter*1000, ev.stats.PacketsLost, route),
			})
		}
	case "audio_error":
		a.emitError(protocol.CodeUnavailable, "audio device: "+errString(ev.err))
		a.endWithReason("audio_unavailable")
	}
}

func (a *actor) reportStatus() {
	if a.peer == nil || a.sig == nil || a.callID == "" {
		return
	}
	pcState, iceState := a.peer.states()
	snap := a.peer.snapshot()
	payload := protocol.ConnectionStatusPayload{
		PCState:      pcState,
		ICEState:     iceState,
		MediaState:   a.peer.mediaState(),
		Relay:        snap.Relay,
		RTTMillis:    snap.RTT * 1000,
		JitterMillis: snap.Jitter * 1000,
		PacketsLost:  snap.PacketsLost,
		PacketsRecv:  snap.PacketsRecv,
		RestartCount: a.peer.restartCount(),
	}
	raw, _ := marshal(payload)
	env := protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeConnectionStatus,
		RequestID: a.requestID(),
		CallID:    a.callID,
		Payload:   raw,
	}
	if err := a.sig.Send(env); err != nil {
		a.cfg.Logger.Debug("status not sent", "error", err.Error())
	}
}

func (a *actor) endWithReason(reason string) {
	if a.sig == nil || a.callID == "" {
		return
	}
	if a.ended {
		return
	}
	a.ended = true
	raw, _ := marshal(protocol.CallEndPayload{Reason: reason})
	_ = a.sig.Send(protocol.Envelope{
		Version:   protocol.Version,
		Type:      protocol.TypeCallEnd,
		RequestID: a.requestID(),
		CallID:    a.callID,
		Payload:   raw,
	})
}

func (a *actor) pushPeer(ev peerEvent) {
	select {
	case a.peerIn <- ev:
	case <-a.done:
	}
}

func (a *actor) closePeer() {
	if a.peer == nil {
		return
	}
	a.peer.close()
	a.peer = nil
}

func (a *actor) close() {
	a.closePeer()
	if a.sig != nil {
		a.sig.Close()
		a.sig = nil
	}
}
