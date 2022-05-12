// adapted from Skanehira's docui package which is no longer maintained

package streamer

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/moby/term"
	"github.com/sirupsen/logrus"
)

type ResizeContainer func(ctx context.Context, id string, options types.ResizeOptions) error

var (
	ErrEmptyExecID   = errors.New("empty exec id")
	ErrTtySizeIsZero = errors.New("tty size is 0")
)

type Streamer struct {
	In    *In
	Out   *Out
	Err   io.Writer
	isTty bool
	Log   *logrus.Entry
}

func New(logger *logrus.Entry) *Streamer {
	return &Streamer{
		In:  NewIn(os.Stdin),
		Out: NewOut(os.Stdout),
		Err: os.Stderr,
		Log: logger,
	}
}

func (s *Streamer) Stream(ctx context.Context, id string, resp types.HijackedResponse, resize ResizeContainer) (err error) {
	if id == "" {
		return ErrEmptyExecID
	}

	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		errCh <- s.stream(ctx, resp)
	}()

	if s.In.IsTerminal {
		s.monitorTtySize(ctx, resize, id)
	}

	if err := <-errCh; err != nil {
		s.Log.Errorf("stream error: %s", err)
		return err
	}

	return nil
}

func (s *Streamer) stream(ctx context.Context, resp types.HijackedResponse) error {
	// set raw mode
	restore, err := s.SetRawTerminal()
	if err != nil {
		return err
	}
	defer restore()

	// start stdin/stdout stream
	outDone := s.streamOut(restore, resp)
	inDone := s.streamIn(restore, resp)

	select {
	case err := <-outDone:
		return err
	case <-inDone:
		select {
		case err := <-outDone:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Streamer) streamIn(restore func(), resp types.HijackedResponse) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		s.Log.Warn("in streamIn")
		defer close(done)
		defer restore()
		_, err := io.Copy(resp.Conn, s.In)

		if _, ok := err.(term.EscapeError); ok {
			s.Log.Warn("not returning from here")
			return
		}

		if err != nil {
			s.Log.Errorf("in stream error: %s", err)
			return
		}

		if err := resp.CloseWrite(); err != nil {
			s.Log.Errorf("close response error: %s", err)
		}
	}()

	return done
}

func (s *Streamer) streamOut(restore func(), resp types.HijackedResponse) <-chan error {
	done := make(chan error, 1)

	go func() {
		_, err := io.Copy(s.Out, resp.Reader)
		restore()

		if err != nil {
			s.Log.Errorf("output stream error: %s", err)
			return
		}

		done <- err
	}()

	return done
}

func (s *Streamer) SetRawTerminal() (func(), error) {
	if err := s.In.SetRawTerminal(); err != nil {
		return nil, err
	}

	var once sync.Once
	restore := func() {
		once.Do(func() {
			if err := s.In.RestoreTerminal(); err != nil {
				s.Log.Errorf("failed to restore terminal: %s\n", err)
			}
		})
	}

	return restore, nil
}

func (s *Streamer) resizeTty(ctx context.Context, resize ResizeContainer, id string) error {
	h, w := s.Out.GetTtySize()
	if h == 0 && w == 0 {
		return ErrTtySizeIsZero
	}

	options := types.ResizeOptions{
		Height: h,
		Width:  w,
	}

	return resize(ctx, id, options)
}

func (s *Streamer) initTtySize(ctx context.Context, resize ResizeContainer, id string) {
	if err := s.resizeTty(ctx, resize, id); err != nil {
		go func() {
			s.Log.Errorf("failed to resize tty: %s\n", err)
			for retry := 0; retry < 5; retry++ {
				time.Sleep(10 * time.Millisecond)
				if err = s.resizeTty(ctx, resize, id); err == nil {
					break
				}
			}
			if err != nil {
				log.Println("failed to resize tty, using default size")
			}
		}()
	}
}
