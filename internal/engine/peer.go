package engine

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/Alice088/worm/internal/audio"
)

const l16PayloadType = 111

func l16Capability() webrtc.RTPCodecCapability {
	return webrtc.RTPCodecCapability{
		MimeType:  "audio/L16",
		ClockRate: audio.SampleRate,
		Channels:  audio.Channels,
	}
}

type statsSnapshot struct {
	RTT         float64
	Jitter      float64
	PacketsLost int64
	PacketsRecv int64
	Relay       bool
}

type peerConfig struct {
	CallID       string
	ICEServers   []webrtc.ICEServer
	Audio        audio.Opener
	Logger       *slog.Logger
	Stats        time.Duration
	OnCandidate  func(webrtcCandidate)
	OnGatherEnd  func()
	OnState      func(pcState, iceState string)
	OnRestart    func(offer string) error
	OnStats      func(stats statsSnapshot, restarts int)
	OnAudioError func(error)
	Tune         func(*webrtc.SettingEngine)
}

type peerSession struct {
	cfg   peerConfig
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticRTP
	log   *slog.Logger

	mu        sync.Mutex
	source    audio.Source
	sink      audio.Sink
	muted     bool
	remoteSet bool
	pending   []webrtc.ICECandidateInit
	restarts  int

	statsMu   sync.Mutex
	stats     statsSnapshot
	closed    bool
	quit      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func newPeer(cfg peerConfig) (*peerSession, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Stats <= 0 {
		cfg.Stats = 5 * time.Second
	}

	pc, err := newAPI(cfg.Tune).NewPeerConnection(webrtc.Configuration{ICEServers: cfg.ICEServers})
	if err != nil {
		return nil, err
	}

	track, err := webrtc.NewTrackLocalStaticRTP(l16Capability(), "audio", cfg.CallID)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	if _, err := pc.AddTrack(track); err != nil {
		_ = pc.Close()
		return nil, err
	}

	p := &peerSession{cfg: cfg, pc: pc, track: track, log: cfg.Logger, quit: make(chan struct{})}

	if p.cfg.Audio.Source != nil {
		if src, err := p.cfg.Audio.Source(); err != nil {
			p.reportAudioError(err)
		} else {
			p.source = src
		}
	}
	if p.cfg.Audio.Sink != nil {
		if sink, err := p.cfg.Audio.Sink(); err != nil {
			p.reportAudioError(err)
		} else {
			p.sink = sink
		}
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			if p.cfg.OnGatherEnd != nil {
				p.cfg.OnGatherEnd()
			}
			return
		}
		if p.cfg.OnCandidate != nil {
			init := c.ToJSON()
			p.cfg.OnCandidate(webrtcCandidate{
				Candidate:        init.Candidate,
				SDPMid:           init.SDPMid,
				SDPMLineIndex:    init.SDPMLineIndex,
				UsernameFragment: init.UsernameFragment,
			})
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		p.emitState()
		if state == webrtc.PeerConnectionStateFailed {
			p.requestRestart()
		}
	})
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		p.emitState()
		if state == webrtc.ICEConnectionStateFailed {
			p.requestRestart()
		}
	})
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		p.wg.Add(1)
		go p.receiveLoop(track)
	})

	p.wg.Add(2)
	go p.sendLoop()
	go p.statsLoop()

	return p, nil
}

func (p *peerSession) emitState() {
	if p.cfg.OnState == nil {
		return
	}
	p.cfg.OnState(p.pc.ConnectionState().String(), p.pc.ICEConnectionState().String())
}

func (p *peerSession) reportAudioError(err error) {
	if p.cfg.OnAudioError != nil {
		p.cfg.OnAudioError(err)
	}
}

func (p *peerSession) sendLoop() {
	defer p.wg.Done()

	frame := make([]byte, audio.FrameSize)
	var (
		seq  uint16
		ts   uint32
		ssrc = uint32(time.Now().UnixNano())
	)
	empty := make([]byte, audio.FrameSize)

	for {
		select {
		case <-p.quit:
			return
		default:
		}

		src := p.currentSource()
		var (
			n   int
			err error
		)
		if src != nil {
			n, err = io.ReadFull(src, frame)
		} else {
			time.Sleep(audio.FrameMs * time.Millisecond)
			frame = empty
			n = len(frame)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				time.Sleep(audio.FrameMs * time.Millisecond)
				continue
			}
			p.reportAudioError(err)
			time.Sleep(audio.FrameMs * time.Millisecond)
			continue
		}

		payload := frame[:n]
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    l16PayloadType,
				SequenceNumber: seq,
				Timestamp:      ts,
				SSRC:           ssrc,
			},
			Payload: payload,
		}
		if err := p.track.WriteRTP(packet); err != nil {
			p.log.Debug("write rtp failed", "error", err.Error())
		}
		seq++
		ts += uint32(n / 2 / audio.Channels)
	}
}

func (p *peerSession) currentSource() audio.Source {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.source
}

func (p *peerSession) receiveLoop(track *webrtc.TrackRemote) {
	defer p.wg.Done()
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		p.statsMu.Lock()
		p.stats.PacketsRecv++
		p.statsMu.Unlock()

		p.mu.Lock()
		sink := p.sink
		p.mu.Unlock()
		if sink != nil {
			if _, err := sink.Write(packet.Payload); err != nil {
				p.reportAudioError(err)
				p.mu.Lock()
				p.sink = nil
				p.mu.Unlock()
			}
		}
	}
}

func (p *peerSession) statsLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.Stats)
	defer ticker.Stop()
	for {
		select {
		case <-p.quit:
			return
		case <-ticker.C:
			p.collectStats()
			if p.cfg.OnStats != nil {
				p.cfg.OnStats(p.snapshot(), p.restartCount())
			}
		}
	}
}

func (p *peerSession) collectStats() {
	report := p.pc.GetStats()
	snapshot := statsSnapshot{}
	for _, stat := range report {
		switch s := stat.(type) {
		case webrtc.ICECandidatePairStats:
			if s.Nominated && s.State == webrtc.StatsICECandidatePairStateSucceeded {
				snapshot.RTT = s.CurrentRoundTripTime
			}
		case webrtc.InboundRTPStreamStats:
			if s.Kind == "audio" {
				snapshot.Jitter = s.Jitter
				snapshot.PacketsLost = int64(s.PacketsLost)
				snapshot.PacketsRecv = int64(s.PacketsReceived)
			}
		case webrtc.ICECandidateStats:
			if s.CandidateType == webrtc.ICECandidateTypeRelay {
				snapshot.Relay = true
			}
		}
	}

	p.statsMu.Lock()
	if snapshot.PacketsRecv < p.stats.PacketsRecv {
		snapshot.PacketsRecv = p.stats.PacketsRecv
	}
	p.stats = snapshot
	p.statsMu.Unlock()
}

func (p *peerSession) snapshot() statsSnapshot {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats
}

func (p *peerSession) createOffer(restart bool) (string, error) {
	offer, err := p.pc.CreateOffer(&webrtc.OfferOptions{ICERestart: restart})
	if err != nil {
		return "", err
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return "", err
	}
	return offer.SDP, nil
}

func (p *peerSession) acceptOffer(sdp string) (string, error) {
	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return "", err
	}
	p.mu.Lock()
	p.remoteSet = true
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()

	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	p.applyCandidates(pending)
	return answer.SDP, nil
}

func (p *peerSession) acceptAnswer(sdp string) error {
	answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}
	if err := p.pc.SetRemoteDescription(answer); err != nil {
		return err
	}
	p.mu.Lock()
	p.remoteSet = true
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	p.applyCandidates(pending)
	return nil
}

func (p *peerSession) addCandidate(init webrtc.ICECandidateInit) {
	p.mu.Lock()
	if !p.remoteSet {
		p.pending = append(p.pending, init)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if err := p.pc.AddICECandidate(init); err != nil {
		p.log.Debug("add ice candidate failed", "error", err.Error())
	}
}

func (p *peerSession) applyCandidates(list []webrtc.ICECandidateInit) {
	for _, c := range list {
		if err := p.pc.AddICECandidate(c); err != nil {
			p.log.Debug("add buffered candidate failed", "error", err.Error())
		}
	}
}

func (p *peerSession) endOfCandidates() {
	_ = p.pc.AddICECandidate(webrtc.ICECandidateInit{})
}

func (p *peerSession) requestRestart() {
	p.mu.Lock()
	if p.closed || p.restarts >= 2 {
		p.mu.Unlock()
		return
	}
	p.restarts++
	attempt := p.restarts
	p.mu.Unlock()

	if p.cfg.OnRestart == nil {
		return
	}
	offer, err := p.createOffer(true)
	if err != nil {
		p.log.Warn("ice restart failed", "attempt", attempt, "error", err.Error())
		return
	}
	if err := p.cfg.OnRestart(offer); err != nil {
		p.log.Warn("renegotiation send failed", "attempt", attempt, "error", err.Error())
	}
}

func (p *peerSession) restartCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.restarts
}

func (p *peerSession) setMuted(muted bool) {
	p.mu.Lock()
	if p.closed || p.muted == muted {
		p.mu.Unlock()
		return
	}
	p.muted = muted
	src := p.source
	p.mu.Unlock()

	if muted {
		if src != nil {
			_ = src.Close()
		}
		p.mu.Lock()
		if p.source == src {
			p.source = nil
		}
		p.mu.Unlock()
		return
	}
	if p.cfg.Audio.Source == nil {
		return
	}
	next, err := p.cfg.Audio.Source()
	if err != nil {
		p.reportAudioError(err)
		return
	}
	p.mu.Lock()
	p.source = next
	p.mu.Unlock()
}

func (p *peerSession) mediaState() string {
	p.mu.Lock()
	hasAudio := p.source != nil || p.sink != nil
	p.mu.Unlock()

	pc := p.pc.ConnectionState()
	ice := p.pc.ICEConnectionState()

	switch {
	case !hasAudio:
		return "unavailable"
	case pc == webrtc.PeerConnectionStateConnected &&
		(ice == webrtc.ICEConnectionStateConnected || ice == webrtc.ICEConnectionStateCompleted):
		return "flowing"
	case pc == webrtc.PeerConnectionStateConnecting || pc == webrtc.PeerConnectionStateNew:
		return "starting"
	case pc == webrtc.PeerConnectionStateDisconnected:
		return "stalled"
	default:
		return "inactive"
	}
}

func (p *peerSession) states() (string, string) {
	return p.pc.ConnectionState().String(), p.pc.ICEConnectionState().String()
}

func (p *peerSession) close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		src, sink := p.source, p.sink
		p.source, p.sink = nil, nil
		p.mu.Unlock()

		close(p.quit)
		if src != nil {
			_ = src.Close()
		}
		if sink != nil {
			_ = sink.Close()
		}
		_ = p.pc.Close()
		p.wg.Wait()
	})
}
