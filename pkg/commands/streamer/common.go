package streamer

import "github.com/moby/term"

type CommonStream struct {
	Fd         uintptr
	IsTerminal bool
	State      *term.State
}
