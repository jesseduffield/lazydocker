package mpb

import (
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8/decor"
	"github.com/vbauerster/mpb/v8/internal"
)

const (
	positionLeft = 1 + iota
	positionRight
)

var defaultSpinnerStyle = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerStyleComposer interface.
type SpinnerStyleComposer interface {
	BarFillerBuilder
	PositionLeft() SpinnerStyleComposer
	PositionRight() SpinnerStyleComposer
	Meta(func(string) string) SpinnerStyleComposer
}

type spinnerFiller struct {
	frames   []string
	count    uint
	meta     func(string) string
	position func(string, int) string
}

type spinnerStyle struct {
	position uint
	frames   []string
	meta     func(string) string
}

// SpinnerStyle constructs default spinner style which can be altered via
// SpinnerStyleComposer interface.
func SpinnerStyle(frames ...string) SpinnerStyleComposer {
	var ss spinnerStyle
	if len(frames) != 0 {
		ss.frames = frames
	} else {
		ss.frames = defaultSpinnerStyle[:]
	}
	return ss
}

func (s spinnerStyle) PositionLeft() SpinnerStyleComposer {
	s.position = positionLeft
	return s
}

func (s spinnerStyle) PositionRight() SpinnerStyleComposer {
	s.position = positionRight
	return s
}

func (s spinnerStyle) Meta(fn func(string) string) SpinnerStyleComposer {
	s.meta = fn
	return s
}

func (s spinnerStyle) Build() BarFiller {
	sf := &spinnerFiller{frames: s.frames}
	switch s.position {
	case positionLeft:
		sf.position = func(frame string, padWidth int) string {
			return frame + strings.Repeat(" ", padWidth)
		}
	case positionRight:
		sf.position = func(frame string, padWidth int) string {
			return strings.Repeat(" ", padWidth) + frame
		}
	default:
		sf.position = func(frame string, padWidth int) string {
			return strings.Repeat(" ", padWidth/2) + frame + strings.Repeat(" ", padWidth/2+padWidth%2)
		}
	}
	if s.meta != nil {
		sf.meta = s.meta
	} else {
		sf.meta = func(s string) string { return s }
	}
	return sf
}

func (s *spinnerFiller) Fill(w io.Writer, stat decor.Statistics) error {
	width := internal.CheckRequestedWidth(stat.RequestedWidth, stat.AvailableWidth)
	frame := s.frames[s.count%uint(len(s.frames))]
	frameWidth := runewidth.StringWidth(frame)
	s.count++

	if width < frameWidth {
		return nil
	}

	_, err := io.WriteString(w, s.position(s.meta(frame), width-frameWidth))
	return err
}
