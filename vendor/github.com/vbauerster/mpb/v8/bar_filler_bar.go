package mpb

import (
	"io"

	"github.com/mattn/go-runewidth"
	"github.com/vbauerster/mpb/v8/decor"
	"github.com/vbauerster/mpb/v8/internal"
)

const (
	iLbound = iota
	iRefiller
	iFiller
	iTip
	iPadding
	iRbound
	iLen
)

var defaultBarStyle = [iLen]string{"[", "+", "=", ">", "-", "]"}

// BarStyleComposer interface.
type BarStyleComposer interface {
	BarFillerBuilder
	Lbound(string) BarStyleComposer
	LboundMeta(func(string) string) BarStyleComposer
	Rbound(string) BarStyleComposer
	RboundMeta(func(string) string) BarStyleComposer
	Filler(string) BarStyleComposer
	FillerMeta(func(string) string) BarStyleComposer
	Refiller(string) BarStyleComposer
	RefillerMeta(func(string) string) BarStyleComposer
	Padding(string) BarStyleComposer
	PaddingMeta(func(string) string) BarStyleComposer
	Tip(frames ...string) BarStyleComposer
	TipMeta(func(string) string) BarStyleComposer
	TipOnComplete() BarStyleComposer
	Reverse() BarStyleComposer
}

type component struct {
	width int
	bytes []byte
}

type barSection struct {
	meta  func(string) string
	bytes []byte
}

type barSections [iLen]barSection

type barFiller struct {
	components [iLen]component
	metas      [iLen]func(string) string
	flushOp    func(barSections, io.Writer) error
	tip        struct {
		onComplete bool
		count      uint
		frames     []component
	}
}

type barStyle struct {
	style         [iLen]string
	metas         [iLen]func(string) string
	tipFrames     []string
	tipOnComplete bool
	rev           bool
}

// BarStyle constructs default bar style which can be altered via
// BarStyleComposer interface.
func BarStyle() BarStyleComposer {
	bs := barStyle{
		style:     defaultBarStyle,
		tipFrames: []string{defaultBarStyle[iTip]},
	}
	return bs
}

func (s barStyle) Lbound(bound string) BarStyleComposer {
	s.style[iLbound] = bound
	return s
}

func (s barStyle) LboundMeta(fn func(string) string) BarStyleComposer {
	s.metas[iLbound] = fn
	return s
}

func (s barStyle) Rbound(bound string) BarStyleComposer {
	s.style[iRbound] = bound
	return s
}

func (s barStyle) RboundMeta(fn func(string) string) BarStyleComposer {
	s.metas[iRbound] = fn
	return s
}

func (s barStyle) Filler(filler string) BarStyleComposer {
	s.style[iFiller] = filler
	return s
}

func (s barStyle) FillerMeta(fn func(string) string) BarStyleComposer {
	s.metas[iFiller] = fn
	return s
}

func (s barStyle) Refiller(refiller string) BarStyleComposer {
	s.style[iRefiller] = refiller
	return s
}

func (s barStyle) RefillerMeta(fn func(string) string) BarStyleComposer {
	s.metas[iRefiller] = fn
	return s
}

func (s barStyle) Padding(padding string) BarStyleComposer {
	s.style[iPadding] = padding
	return s
}

func (s barStyle) PaddingMeta(fn func(string) string) BarStyleComposer {
	s.metas[iPadding] = fn
	return s
}

func (s barStyle) Tip(frames ...string) BarStyleComposer {
	if len(frames) != 0 {
		s.tipFrames = frames
	}
	return s
}

func (s barStyle) TipMeta(fn func(string) string) BarStyleComposer {
	s.metas[iTip] = fn
	return s
}

func (s barStyle) TipOnComplete() BarStyleComposer {
	s.tipOnComplete = true
	return s
}

func (s barStyle) Reverse() BarStyleComposer {
	s.rev = true
	return s
}

func (s barStyle) Build() BarFiller {
	bf := &barFiller{metas: s.metas}
	bf.components[iLbound] = component{
		width: runewidth.StringWidth(s.style[iLbound]),
		bytes: []byte(s.style[iLbound]),
	}
	bf.components[iRbound] = component{
		width: runewidth.StringWidth(s.style[iRbound]),
		bytes: []byte(s.style[iRbound]),
	}
	bf.components[iFiller] = component{
		width: runewidth.StringWidth(s.style[iFiller]),
		bytes: []byte(s.style[iFiller]),
	}
	bf.components[iRefiller] = component{
		width: runewidth.StringWidth(s.style[iRefiller]),
		bytes: []byte(s.style[iRefiller]),
	}
	bf.components[iPadding] = component{
		width: runewidth.StringWidth(s.style[iPadding]),
		bytes: []byte(s.style[iPadding]),
	}
	bf.tip.onComplete = s.tipOnComplete
	bf.tip.frames = make([]component, 0, len(s.tipFrames))
	for _, t := range s.tipFrames {
		bf.tip.frames = append(bf.tip.frames, component{
			width: runewidth.StringWidth(t),
			bytes: []byte(t),
		})
	}
	if s.rev {
		bf.flushOp = barSections.flushRev
	} else {
		bf.flushOp = barSections.flush
	}
	return bf
}

func (s *barFiller) Fill(w io.Writer, stat decor.Statistics) error {
	width := internal.CheckRequestedWidth(stat.RequestedWidth, stat.AvailableWidth)
	// don't count brackets as progress
	width -= (s.components[iLbound].width + s.components[iRbound].width)
	if width < 0 {
		return nil
	}

	var tip component
	var refilling, filling, padding []byte
	var fillCount int
	curWidth := int(internal.PercentageRound(stat.Total, stat.Current, uint(width)))

	if curWidth != 0 {
		if !stat.Completed || s.tip.onComplete {
			tip = s.tip.frames[s.tip.count%uint(len(s.tip.frames))]
			s.tip.count++
			fillCount += tip.width
		}
		switch refWidth := 0; {
		case stat.Refill != 0:
			refWidth = int(internal.PercentageRound(stat.Total, stat.Refill, uint(width)))
			curWidth -= refWidth
			refWidth += curWidth
			fallthrough
		default:
			for w := s.components[iFiller].width; curWidth-fillCount >= w; fillCount += w {
				filling = append(filling, s.components[iFiller].bytes...)
			}
			for w := s.components[iRefiller].width; refWidth-fillCount >= w; fillCount += w {
				refilling = append(refilling, s.components[iRefiller].bytes...)
			}
		}
	}

	for w := s.components[iPadding].width; width-fillCount >= w; fillCount += w {
		padding = append(padding, s.components[iPadding].bytes...)
	}

	for w := 1; width-fillCount >= w; fillCount += w {
		padding = append(padding, "…"...)
	}

	return s.flushOp(barSections{
		{s.metas[iLbound], s.components[iLbound].bytes},
		{s.metas[iRefiller], refilling},
		{s.metas[iFiller], filling},
		{s.metas[iTip], tip.bytes},
		{s.metas[iPadding], padding},
		{s.metas[iRbound], s.components[iRbound].bytes},
	}, w)
}

func (s barSection) flush(w io.Writer) (err error) {
	if s.meta != nil {
		_, err = io.WriteString(w, s.meta(string(s.bytes)))
	} else {
		_, err = w.Write(s.bytes)
	}
	return err
}

func (bb barSections) flush(w io.Writer) error {
	for _, s := range bb {
		err := s.flush(w)
		if err != nil {
			return err
		}
	}
	return nil
}

func (bb barSections) flushRev(w io.Writer) error {
	bb[0], bb[len(bb)-1] = bb[len(bb)-1], bb[0]
	for i := len(bb) - 1; i >= 0; i-- {
		err := bb[i].flush(w)
		if err != nil {
			return err
		}
	}
	return nil
}
