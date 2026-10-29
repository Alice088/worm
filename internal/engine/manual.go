package engine

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/Alice088/worm/internal/protocol"
	"github.com/Alice088/worm/internal/session"
)

type Manual struct {
	cfg    Config
	events chan Event

	mu         sync.Mutex
	opMu       sync.Mutex
	peer       *peerSession
	gathered   chan struct{}
	candidates []string
	local      session.Signal
	audioWarn  sync.Once
	closed     bool
}

func NewManual(cfg Config) *Manual {
	cfg.withDefaults()
	if len(cfg.ICEServers) == 0 {
		cfg.ICEServers = []protocol.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}
	return &Manual{cfg: cfg, events: make(chan Event, 32)}
}

func (m *Manual) Events() <-chan Event { return m.events }

func (m *Manual) Offer(ctx context.Context) (session.Signal, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	cached := m.local
	m.mu.Unlock()
	if cached.Kind == session.SignalOffer {
		return cached, nil
	}

	peer, err := m.ensurePeer()
	if err != nil {
		return session.Signal{}, err
	}
	sdp, err := peer.createOffer(false)
	if err != nil {
		return session.Signal{}, err
	}
	m.awaitGathering(ctx)
	sig := session.Signal{Kind: session.SignalOffer, SDP: sdp, Candidates: m.takeCandidates()}

	m.mu.Lock()
	m.local = sig
	m.mu.Unlock()
	return sig, nil
}

func (m *Manual) Answer(ctx context.Context, offer session.Signal) (session.Signal, error) {
	if offer.Kind != session.SignalOffer {
		return session.Signal{}, errors.New("wrong signal kind: expected an offer code")
	}

	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	cached := m.local
	m.mu.Unlock()
	if cached.Kind == session.SignalAnswer {
		return cached, nil
	}

	peer, err := m.ensurePeer()
	if err != nil {
		return session.Signal{}, err
	}
	if err := m.applyRemote(peer, offer); err != nil {
		return session.Signal{}, err
	}
	sdp, err := peer.acceptOffer(offer.SDP)
	if err != nil {
		return session.Signal{}, err
	}
	m.awaitGathering(ctx)
	sig := session.Signal{Kind: session.SignalAnswer, SDP: sdp, Candidates: m.takeCandidates()}

	m.mu.Lock()
	m.local = sig
	m.mu.Unlock()
	return sig, nil
}

func (m *Manual) Connect(answer session.Signal) error {
	if answer.Kind != session.SignalAnswer {
		return errors.New("wrong signal kind: expected an answer code")
	}
	m.mu.Lock()
	peer := m.peer
	m.mu.Unlock()
	if peer == nil {
		return errors.New("create an offer first")
	}
	if err := m.applyRemote(peer, answer); err != nil {
		return err
	}
	return peer.acceptAnswer(answer.SDP)
}

func (m *Manual) AddCandidates(code session.Signal) error {
	if len(code.Candidates) == 0 {
		return errors.New("that code has no candidates")
	}
	m.mu.Lock()
	peer := m.peer
	m.mu.Unlock()
	if peer == nil {
		return errors.New("answer an offer first")
	}
	for _, c := range code.Candidates {
		peer.addCandidate(webrtcCandidateInit(protocol.ICECandidatePayload{Candidate: c}))
	}
	return nil
}

func (m *Manual) applyRemote(peer *peerSession, sig session.Signal) error {
	for _, c := range sig.Candidates {
		peer.addCandidate(webrtcCandidateInit(protocol.ICECandidatePayload{Candidate: c}))
	}
	return nil
}

func (m *Manual) SetMuted(muted bool) {
	m.mu.Lock()
	peer := m.peer
	m.mu.Unlock()
	if peer != nil {
		peer.setMuted(muted)
	}
}

func (m *Manual) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	peer := m.peer
	m.peer = nil
	m.mu.Unlock()

	if peer != nil {
		peer.close()
	}
	close(m.events)
	return nil
}

func (m *Manual) ensurePeer() (*peerSession, error) {
	m.mu.Lock()
	if m.peer != nil {
		peer := m.peer
		m.mu.Unlock()
		return peer, nil
	}
	m.mu.Unlock()

	callID := "manual-" + session.RandomID(6)
	gathered := make(chan struct{})
	once := &sync.Once{}

	peer, err := newPeer(peerConfig{
		CallID:     callID,
		ICEServers: toICEServers(m.cfg.ICEServers),
		Audio:      m.cfg.Audio,
		Logger:     m.cfg.Logger,
		Stats:      m.cfg.StatsInterval,
		Tune:       m.cfg.Tune,
		OnCandidate: func(c webrtcCandidate) {
			m.mu.Lock()
			m.candidates = append(m.candidates, c.Candidate)
			m.mu.Unlock()
		},
		OnGatherEnd: func() { once.Do(func() { close(gathered) }) },
		OnState: func(pc, ice string) {
			m.emit(Event{Kind: EventMedia, Message: "media " + pc + "/" + ice, Connected: pc == "connected"})
		},
		OnStats: func(stats statsSnapshot, _ int) {
			route := "direct"
			if stats.Relay {
				route = "relayed via turn"
			}
			m.emit(Event{
				Kind: EventStats,
				Message: "rtt " + ms(stats.RTT) + " · jitter " + ms(stats.Jitter) +
					" · loss " + itoa(stats.PacketsLost) + " · " + route,
			})
		},
		OnAudioError: func(err error) {
			m.audioWarn.Do(func() {
				m.emit(Event{Kind: EventNotice, Message: err.Error() + " · continuing without sound"})
			})
		},
	})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.peer = peer
	m.gathered = gathered
	m.mu.Unlock()
	return peer, nil
}

func (m *Manual) awaitGathering(ctx context.Context) {
	m.mu.Lock()
	ch := m.gathered
	m.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
		m.emit(Event{Kind: EventNotice, Message: "ice gathering timed out, sending what we have"})
	}
}

func (m *Manual) takeCandidates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]string(nil), m.candidates...)
	m.candidates = nil
	return out
}

func (m *Manual) emit(ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	select {
	case m.events <- ev:
	default:
	}
}

func ms(seconds float64) string {
	return strconv.FormatInt(int64(seconds*1000), 10) + "ms"
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
