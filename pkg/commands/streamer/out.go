package streamer

import (
	"io"
	"log"

	"github.com/moby/term"
)

type Out struct {
	CommonStream
	out io.Writer
}

func (o *Out) Write(p []byte) (int, error) {
	return o.out.Write(p)
}

func (o *Out) GetTtySize() (uint, uint) {
	if !o.IsTerminal {
		return 0, 0
	}
	ws, err := term.GetWinsize(o.Fd)
	if err != nil {
		log.Printf("getting tty size: %s\n", err)
		return 0, 0
	}
	return uint(ws.Height), uint(ws.Width)
}

func NewOut(out io.Writer) *Out {
	fd, isTerminal := term.GetFdInfo(out)
	return &Out{
		out: out,
		CommonStream: CommonStream{
			Fd:         fd,
			IsTerminal: isTerminal,
		},
	}
}
