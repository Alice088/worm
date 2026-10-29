package audio

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	SampleRate = 48000
	Channels   = 1
	FrameMs    = 10
	FrameSize  = SampleRate / 1000 * FrameMs * Channels * 2
)

type Source interface {
	Read(p []byte) (int, error)
	Close() error
}

type Sink interface {
	Write(p []byte) (int, error)
	Close() error
}

type Opener struct {
	Source func() (Source, error)
	Sink   func() (Sink, error)
}

var Default = Opener{Source: openSource, Sink: openSink}

func deviceArgs() []string {
	return []string{"-q", "-t", "raw", "-f", "S16_LE", "-r", "48000", "-c", "1", "-"}
}

type execSource struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	once   sync.Once
}

func openSource() (Source, error) {
	if _, err := exec.LookPath("arecord"); err != nil {
		return nil, fmt.Errorf("audio capture unavailable: arecord not found")
	}
	cmd := exec.Command("arecord", deviceArgs()...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execSource{cmd: cmd, stdout: stdout}, nil
}

func (s *execSource) Read(p []byte) (int, error) { return s.stdout.Read(p) }

func (s *execSource) Close() error {
	s.once.Do(func() {
		_ = s.stdout.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
	return nil
}

type execSink struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	once  sync.Once
}

func openSink() (Sink, error) {
	if _, err := exec.LookPath("aplay"); err != nil {
		return nil, fmt.Errorf("audio playback unavailable: aplay not found")
	}
	cmd := exec.Command("aplay", deviceArgs()...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execSink{cmd: cmd, stdin: stdin}, nil
}

func (s *execSink) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *execSink) Close() error {
	s.once.Do(func() {
		_ = s.stdin.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
	return nil
}

type Discard struct{}

func (Discard) Write(p []byte) (int, error) { return len(p), nil }
func (Discard) Close() error                { return nil }

type NullSource struct {
	frame  []byte
	delay  time.Duration
	mu     sync.Mutex
	closed bool
}

func NewNullSource() *NullSource {
	return &NullSource{frame: make([]byte, FrameSize), delay: FrameMs * time.Millisecond}
}

func (s *NullSource) SetDelay(d time.Duration) { s.delay = d }

func (s *NullSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, io.EOF
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func (s *NullSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
