package decor

import (
	"fmt"
	"math"
	"time"

	"github.com/VividCortex/ewma"
)

var (
	_ Decorator        = (*movingAverageETA)(nil)
	_ EwmaDecorator    = (*movingAverageETA)(nil)
	_ Decorator        = (*averageETA)(nil)
	_ AverageDecorator = (*averageETA)(nil)
)

// TimeNormalizer interface. Implementers could be passed into
// MovingAverageETA, in order to affect i.e. normalize its output.
type TimeNormalizer interface {
	Normalize(time.Duration) time.Duration
}

// TimeNormalizerFunc is function type adapter to convert function
// into TimeNormalizer.
type TimeNormalizerFunc func(time.Duration) time.Duration

func (f TimeNormalizerFunc) Normalize(src time.Duration) time.Duration {
	return f(src)
}

// EwmaETA exponential-weighted-moving-average based ETA decorator. For this
// decorator to work correctly you have to measure each iteration's duration
// and pass it to one of the (*Bar).EwmaIncr... family methods.
func EwmaETA(style TimeStyle, age float64, wcc ...WC) Decorator {
	return EwmaNormalizedETA(style, age, nil, wcc...)
}

// EwmaNormalizedETA same as EwmaETA but with TimeNormalizer option.
func EwmaNormalizedETA(style TimeStyle, age float64, normalizer TimeNormalizer, wcc ...WC) Decorator {
	var average ewma.MovingAverage
	if age == 0 {
		average = ewma.NewMovingAverage()
	} else {
		average = ewma.NewMovingAverage(age)
	}
	return MovingAverageETA(style, average, normalizer, wcc...)
}

// MovingAverageETA decorator relies on MovingAverage implementation to calculate its average.
//
//	`style` one of [ET_STYLE_GO|ET_STYLE_HHMMSS|ET_STYLE_HHMM|ET_STYLE_MMSS]
//
//	`average` implementation of MovingAverage interface
//
//	`normalizer` available implementations are [FixedIntervalTimeNormalizer|MaxTolerateTimeNormalizer]
//
//	`wcc` optional WC config
func MovingAverageETA(style TimeStyle, average ewma.MovingAverage, normalizer TimeNormalizer, wcc ...WC) Decorator {
	if average == nil {
		average = NewMedian()
	}
	d := &movingAverageETA{
		WC:         initWC(wcc...),
		producer:   chooseTimeProducer(style),
		average:    average,
		normalizer: normalizer,
	}
	return d
}

type movingAverageETA struct {
	WC
	producer   func(time.Duration) string
	average    ewma.MovingAverage
	normalizer TimeNormalizer
	zDur       time.Duration
}

func (d *movingAverageETA) Decor(s Statistics) (string, int) {
	v := math.Round(d.average.Value())
	remaining := time.Duration((s.Total - s.Current) * int64(v))
	if d.normalizer != nil {
		remaining = d.normalizer.Normalize(remaining)
	}
	return d.Format(d.producer(remaining))
}

func (d *movingAverageETA) EwmaUpdate(n int64, dur time.Duration) {
	if n <= 0 {
		d.zDur += dur
		return
	}
	durPerItem := float64(d.zDur+dur) / float64(n)
	if math.IsInf(durPerItem, 0) || math.IsNaN(durPerItem) {
		d.zDur += dur
		return
	}
	d.zDur = 0
	d.average.Add(durPerItem)
}

// AverageETA decorator. It's wrapper of NewAverageETA.
//
//	`style` one of [ET_STYLE_GO|ET_STYLE_HHMMSS|ET_STYLE_HHMM|ET_STYLE_MMSS]
//
//	`wcc` optional WC config
func AverageETA(style TimeStyle, wcc ...WC) Decorator {
	return NewAverageETA(style, time.Now(), nil, wcc...)
}

// NewAverageETA decorator with user provided start time.
//
//	`style` one of [ET_STYLE_GO|ET_STYLE_HHMMSS|ET_STYLE_HHMM|ET_STYLE_MMSS]
//
//	`start` start time
//
//	`normalizer` available implementations are [FixedIntervalTimeNormalizer|MaxTolerateTimeNormalizer]
//
//	`wcc` optional WC config
func NewAverageETA(style TimeStyle, start time.Time, normalizer TimeNormalizer, wcc ...WC) Decorator {
	d := &averageETA{
		WC:         initWC(wcc...),
		start:      start,
		normalizer: normalizer,
		producer:   chooseTimeProducer(style),
	}
	return d
}

type averageETA struct {
	WC
	start      time.Time
	normalizer TimeNormalizer
	producer   func(time.Duration) string
}

func (d *averageETA) Decor(s Statistics) (string, int) {
	var remaining time.Duration
	if s.Current != 0 {
		durPerItem := float64(time.Since(d.start)) / float64(s.Current)
		durPerItem = math.Round(durPerItem)
		remaining = time.Duration((s.Total - s.Current) * int64(durPerItem))
		if d.normalizer != nil {
			remaining = d.normalizer.Normalize(remaining)
		}
	}
	return d.Format(d.producer(remaining))
}

func (d *averageETA) AverageAdjust(start time.Time) {
	d.start = start
}

// MaxTolerateTimeNormalizer returns implementation of TimeNormalizer.
func MaxTolerateTimeNormalizer(maxTolerate time.Duration) TimeNormalizer {
	var normalized time.Duration
	var lastCall time.Time
	return TimeNormalizerFunc(func(remaining time.Duration) time.Duration {
		if diff := normalized - remaining; diff <= 0 || diff > maxTolerate || remaining < time.Minute {
			normalized = remaining
			lastCall = time.Now()
			return remaining
		}
		normalized -= time.Since(lastCall)
		lastCall = time.Now()
		if normalized > 0 {
			return normalized
		}
		return remaining
	})
}

// FixedIntervalTimeNormalizer returns implementation of TimeNormalizer.
func FixedIntervalTimeNormalizer(updInterval int) TimeNormalizer {
	var normalized time.Duration
	var lastCall time.Time
	var count int
	return TimeNormalizerFunc(func(remaining time.Duration) time.Duration {
		if count == 0 || remaining < time.Minute {
			count = updInterval
			normalized = remaining
			lastCall = time.Now()
			return remaining
		}
		count--
		normalized -= time.Since(lastCall)
		lastCall = time.Now()
		if normalized > 0 {
			return normalized
		}
		return remaining
	})
}

func chooseTimeProducer(style TimeStyle) func(time.Duration) string {
	switch style {
	case ET_STYLE_HHMMSS:
		return func(remaining time.Duration) string {
			hours := int64(remaining/time.Hour) % 60
			minutes := int64(remaining/time.Minute) % 60
			seconds := int64(remaining/time.Second) % 60
			return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
		}
	case ET_STYLE_HHMM:
		return func(remaining time.Duration) string {
			hours := int64(remaining/time.Hour) % 60
			minutes := int64(remaining/time.Minute) % 60
			return fmt.Sprintf("%02d:%02d", hours, minutes)
		}
	case ET_STYLE_MMSS:
		return func(remaining time.Duration) string {
			hours := int64(remaining/time.Hour) % 60
			minutes := int64(remaining/time.Minute) % 60
			seconds := int64(remaining/time.Second) % 60
			if hours > 0 {
				return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
			}
			return fmt.Sprintf("%02d:%02d", minutes, seconds)
		}
	default:
		return func(remaining time.Duration) string {
			return remaining.Truncate(time.Second).String()
		}
	}
}
