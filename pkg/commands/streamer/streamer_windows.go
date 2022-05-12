//go:build windows
// +build windows

package streamer

import (
	"context"
)

func (s *Streamer) monitorTtySize(ctx context.Context, resize ResizeContainer, id string) {
	/* TODO: mattn: Currently, this is not supported on Windows.
	s.initTtySize(ctx, resize, id)
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGWINCH)
	go func() {
		for range sigchan {
			s.resizeTty(ctx, resize, id)
		}
	}()
	*/
}
