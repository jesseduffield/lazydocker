package decor

import (
	"time"
)

// Elapsed decorator. It's wrapper of NewElapsed.
//
//	`style` one of [ET_STYLE_GO|ET_STYLE_HHMMSS|ET_STYLE_HHMM|ET_STYLE_MMSS]
//
//	`wcc` optional WC config
func Elapsed(style TimeStyle, wcc ...WC) Decorator {
	return NewElapsed(style, time.Now(), wcc...)
}

// NewElapsed returns elapsed time decorator.
//
//	`style` one of [ET_STYLE_GO|ET_STYLE_HHMMSS|ET_STYLE_HHMM|ET_STYLE_MMSS]
//
//	`start` start time
//
//	`wcc` optional WC config
func NewElapsed(style TimeStyle, start time.Time, wcc ...WC) Decorator {
	var msg string
	producer := chooseTimeProducer(style)
	fn := func(s Statistics) string {
		if !s.Completed && !s.Aborted {
			msg = producer(time.Since(start))
		}
		return msg
	}
	return Any(fn, wcc...)
}
