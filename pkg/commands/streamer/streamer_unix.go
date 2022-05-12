//go:build !windows
// +build !windows

package streamer

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func (s *Streamer) monitorTtySize(ctx context.Context, resize ResizeContainer, id string) {
	s.initTtySize(ctx, resize, id)
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGWINCH)
	go func() {
		for range sigchan {
			_ = s.resizeTty(ctx, resize, id)
		}
	}()
}
