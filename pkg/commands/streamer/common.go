package streamer

import "github.com/moby/term"

type CommonStream struct {
	Fd         uintptr
	IsTerminal bool
	State      *term.State
}

func (s *CommonStream) RestoreTerminal() {
	if s.State != nil {
		term.RestoreTerminal(s.Fd, s.State)
	}
}
