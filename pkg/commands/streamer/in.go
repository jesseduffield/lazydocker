package streamer

import (
	"io"

	"github.com/moby/term"
)

type In struct {
	CommonStream
	in io.ReadCloser
}

func (i *In) Read(p []byte) (int, error) {
	return i.in.Read(p)
}

func (i *In) Close() error {
	return i.in.Close()
}

func (i *In) SetRawTerminal() error {
	var err error
	i.CommonStream.State, err = term.SetRawTerminal(i.Fd)
	return err
}

func (i *In) RestoreTerminal() error {
	if i.CommonStream.State == nil {
		return nil
	}
	return term.RestoreTerminal(i.CommonStream.Fd, i.CommonStream.State)
}

func NewIn(in io.ReadCloser) *In {
	fd, isTerminal := term.GetFdInfo(in)
	return &In{
		in: in,
		CommonStream: CommonStream{
			Fd:         fd,
			IsTerminal: isTerminal,
		},
	}
}
