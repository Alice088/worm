package client

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Alice088/worm/internal/protocol"
)

var (
	ErrOffline     = errors.New("signaling is offline")
	ErrQueueFull   = errors.New("signaling queue is full")
	ErrClosed      = errors.New("signaling is closed")
	ErrUnreachable = errors.New("signaling server is unreachable")
)

type SignalingConfig struct {
	URL              string
	Token            string
	Logger           *slog.Logger
	MaxAttempts      int
	Backoff          time.Duration
	MaxBackoff       time.Duration
	RecoveryDeadline time.Duration
	PingInterval     time.Duration
	IdleTimeout      time.Duration
	QueueSize        int
	DialTimeout      time.Duration
}

func (c *SignalingConfig) withDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.Backoff <= 0 {
		c.Backoff = 500 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 8 * time.Second
	}
	if c.RecoveryDeadline <= 0 {
		c.RecoveryDeadline = 45 * time.Second
	}
	if c.PingInterval <= 0 {
		c.PingInterval = 15 * time.Second
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 45 * time.Second
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 128
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 10 * time.Second
	}
}

type ConnectionState struct {
	Connected bool
	Attempt   int
	Err       error
	Fatal     bool
}

type Signaling struct {
	cfg SignalingConfig

	out    chan []byte
	in     chan protocol.Envelope
	status chan ConnectionState
	done   chan struct{}

	once      sync.Once
	mu        sync.RWMutex
	connected bool
	conn      *websocket.Conn
	baseCtx   context.Context
	cancel    context.CancelFunc
}

func Dial(ctx context.Context, cfg SignalingConfig) (*Signaling, error) {
	cfg.withDefaults()
	conn, err := dialOnce(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Signaling{
		cfg:    cfg,
		out:    make(chan []byte, cfg.QueueSize),
		in:     make(chan protocol.Envelope, 32),
		status: make(chan ConnectionState, 8),
		done:   make(chan struct{}),
	}
	s.baseCtx, s.cancel = context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn = conn
	s.connected = true
	s.mu.Unlock()

	go s.run(s.baseCtx, conn)
	s.emit(ConnectionState{Connected: true})
	return s, nil
}

func (s *Signaling) In() <-chan protocol.Envelope   { return s.in }
func (s *Signaling) Status() <-chan ConnectionState { return s.status }
func (s *Signaling) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

func (s *Signaling) Send(env protocol.Envelope) error {
	select {
	case <-s.done:
		return ErrClosed
	default:
	}
	data, err := env.Encode()
	if err != nil {
		return err
	}
	select {
	case s.out <- data:
		return nil
	case <-s.done:
		return ErrClosed
	case <-time.After(2 * time.Second):
		return ErrQueueFull
	}
}

func (s *Signaling) Close() {
	s.once.Do(func() {
		if s.Connected() {
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && len(s.out) > 0 {
				time.Sleep(10 * time.Millisecond)
			}
		}
		s.cancel()
		close(s.done)
		s.mu.Lock()
		if s.conn != nil {
			_ = s.conn.Close(websocket.StatusNormalClosure, "client closed")
			s.conn = nil
		}
		s.connected = false
		s.mu.Unlock()
	})
}

func (s *Signaling) run(ctx context.Context, conn *websocket.Conn) {
	defer close(s.in)
	defer close(s.status)

	attempt := 0
	deadline := time.Now().Add(s.cfg.RecoveryDeadline)

	for {
		err := s.serve(ctx, conn)
		s.setConnected(false)

		select {
		case <-s.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		attempt++
		if attempt > s.cfg.MaxAttempts || time.Now().After(deadline) {
			s.emit(ConnectionState{Attempt: attempt, Err: err, Fatal: true})
			s.cfg.Logger.Error("signaling reconnect exhausted", "attempts", attempt, "error", errText(err))
			return
		}
		s.emit(ConnectionState{Attempt: attempt, Err: err})

		backoff := s.backoff(attempt)
		select {
		case <-s.done:
			return
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		next, derr := dialOnce(ctx, s.cfg)
		if derr != nil {
			s.cfg.Logger.Warn("signaling reconnect failed", "attempt", attempt, "error", derr.Error())
			continue
		}
		s.mu.Lock()
		s.conn = next
		s.mu.Unlock()
		conn = next
		s.setConnected(true)
		s.cfg.Logger.Info("signaling reconnected", "attempt", attempt)
		s.emit(ConnectionState{Connected: true, Attempt: attempt})
	}
}

func (s *Signaling) serve(ctx context.Context, conn *websocket.Conn) error {
	readCh := make(chan protocol.Envelope, 16)
	readErr := make(chan error, 1)

	go func() {
		defer close(readCh)
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			if kind != websocket.MessageText {
				continue
			}
			env, err := protocol.Decode(data)
			if err != nil {
				s.cfg.Logger.Warn("signaling: dropping malformed message", "error", err.Error())
				continue
			}
			select {
			case readCh <- env:
			case <-s.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	ping := time.NewTicker(s.cfg.PingInterval)
	defer ping.Stop()
	lastRecv := time.Now()

	for {
		select {
		case <-s.done:
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()

		case err := <-readErr:
			return err

		case env, ok := <-readCh:
			if !ok {
				return errors.New("signaling read loop closed")
			}
			lastRecv = time.Now()
			select {
			case s.in <- env:
			case <-s.done:
				return ErrClosed
			}

		case data := <-s.out:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return err
			}

		case <-ping.C:
			if time.Since(lastRecv) > s.cfg.IdleTimeout {
				return errors.New("signaling idle timeout")
			}
			payload, _ := protocol.Envelope{
				Version: protocol.Version,
				Type:    protocol.TypePing,
				Payload: []byte(`{"nonce":"keepalive"}`),
			}.Encode()
			select {
			case s.out <- payload:
			default:
				s.cfg.Logger.Warn("signaling ping skipped: queue full")
			}
		}
	}
}

func (s *Signaling) setConnected(v bool) {
	s.mu.Lock()
	s.connected = v
	s.mu.Unlock()
}

func (s *Signaling) emit(state ConnectionState) {
	select {
	case s.status <- state:
	case <-s.done:
	}
}

func (s *Signaling) backoff(attempt int) time.Duration {
	d := s.cfg.Backoff
	for i := 1; i < attempt && d < s.cfg.MaxBackoff; i++ {
		d *= 2
	}
	if d > s.cfg.MaxBackoff {
		d = s.cfg.MaxBackoff
	}
	jitter := time.Duration(rand.Int63n(int64(d/4) + 1))
	return d + jitter
}

func dialOnce(ctx context.Context, cfg SignalingConfig) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()

	header := map[string][]string{}
	if cfg.Token != "" {
		header["Authorization"] = []string{"Bearer " + cfg.Token}
	}
	conn, _, err := websocket.Dial(dialCtx, cfg.URL, &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: []string{"worm.v1"},
	})
	if err != nil {
		return nil, errors.Join(ErrUnreachable, err)
	}
	conn.SetReadLimit(protocol.MaxMessageBytes)
	return conn, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
