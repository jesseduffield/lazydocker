package decor

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/VividCortex/ewma"
)

var (
	_ Decorator        = (*movingAverageSpeed)(nil)
	_ EwmaDecorator    = (*movingAverageSpeed)(nil)
	_ Decorator        = (*averageSpeed)(nil)
	_ AverageDecorator = (*averageSpeed)(nil)
)

// FmtAsSpeed adds "/s" to the end of the input formatter. To be
// used with SizeB1000 or SizeB1024 types, for example:
//
//	fmt.Printf("%.1f", FmtAsSpeed(SizeB1024(2048)))
func FmtAsSpeed(input fmt.Formatter) fmt.Formatter {
	return &speedFormatter{input}
}

type speedFormatter struct {
	fmt.Formatter
}

func (s *speedFormatter) Format(st fmt.State, verb rune) {
	s.Formatter.Format(st, verb)
	_, err := io.WriteString(st, "/s")
	if err != nil {
		panic(err)
	}
}

// EwmaSpeed exponential-weighted-moving-average based speed decorator.
// For this decorator to work correctly you have to measure each iteration's
// duration and pass it to one of the (*Bar).EwmaIncr... family methods.
func EwmaSpeed(unit interface{}, format string, age float64, wcc ...WC) Decorator {
	var average ewma.MovingAverage
	if age == 0 {
		average = ewma.NewMovingAverage()
	} else {
		average = ewma.NewMovingAverage(age)
	}
	return MovingAverageSpeed(unit, format, average, wcc...)
}

// MovingAverageSpeed decorator relies on MovingAverage implementation
// to calculate its average.
//
//	`unit` one of [0|SizeB1024(0)|SizeB1000(0)]
//
//	`format` printf compatible verb for value, like "%f" or "%d"
//
//	`average` MovingAverage implementation
//
//	`wcc` optional WC config
//
// format examples:
//
//	unit=SizeB1024(0), format="%.1f"  output: "1.0MiB/s"
//	unit=SizeB1024(0), format="% .1f" output: "1.0 MiB/s"
//	unit=SizeB1000(0), format="%.1f"  output: "1.0MB/s"
//	unit=SizeB1000(0), format="% .1f" output: "1.0 MB/s"
func MovingAverageSpeed(unit interface{}, format string, average ewma.MovingAverage, wcc ...WC) Decorator {
	d := &movingAverageSpeed{
		WC:       initWC(wcc...),
		producer: chooseSpeedProducer(unit, format),
		average:  average,
	}
	return d
}

type movingAverageSpeed struct {
	WC
	producer func(float64) string
	average  ewma.MovingAverage
	zDur     time.Duration
}

func (d *movingAverageSpeed) Decor(_ Statistics) (string, int) {
	var str string
	// ewma implementation may return 0 before accumulating certain number of samples
	if v := d.average.Value(); v != 0 {
		str = d.producer(1e9 / v)
	} else {
		str = d.producer(0)
	}
	return d.Format(str)
}

func (d *movingAverageSpeed) EwmaUpdate(n int64, dur time.Duration) {
	if n <= 0 {
		d.zDur += dur
		return
	}
	durPerByte := float64(d.zDur+dur) / float64(n)
	if math.IsInf(durPerByte, 0) || math.IsNaN(durPerByte) {
		d.zDur += dur
		return
	}
	d.zDur = 0
	d.average.Add(durPerByte)
}

// AverageSpeed decorator with dynamic unit measure adjustment. It's
// a wrapper of NewAverageSpeed.
func AverageSpeed(unit interface{}, format string, wcc ...WC) Decorator {
	return NewAverageSpeed(unit, format, time.Now(), wcc...)
}

// NewAverageSpeed decorator with dynamic unit measure adjustment and
// user provided start time.
//
//	`unit` one of [0|SizeB1024(0)|SizeB1000(0)]
//
//	`format` printf compatible verb for value, like "%f" or "%d"
//
//	`start` start time
//
//	`wcc` optional WC config
//
// format examples:
//
//	unit=SizeB1024(0), format="%.1f"  output: "1.0MiB/s"
//	unit=SizeB1024(0), format="% .1f" output: "1.0 MiB/s"
//	unit=SizeB1000(0), format="%.1f"  output: "1.0MB/s"
//	unit=SizeB1000(0), format="% .1f" output: "1.0 MB/s"
func NewAverageSpeed(unit interface{}, format string, start time.Time, wcc ...WC) Decorator {
	d := &averageSpeed{
		WC:       initWC(wcc...),
		start:    start,
		producer: chooseSpeedProducer(unit, format),
	}
	return d
}

type averageSpeed struct {
	WC
	start    time.Time
	producer func(float64) string
	msg      string
}

func (d *averageSpeed) Decor(s Statistics) (string, int) {
	if !s.Completed {
		speed := float64(s.Current) / float64(time.Since(d.start))
		d.msg = d.producer(speed * 1e9)
	}
	return d.Format(d.msg)
}

func (d *averageSpeed) AverageAdjust(start time.Time) {
	d.start = start
}

func chooseSpeedProducer(unit interface{}, format string) func(float64) string {
	switch unit.(type) {
	case SizeB1024:
		if format == "" {
			format = "% d"
		}
		return func(speed float64) string {
			return fmt.Sprintf(format, FmtAsSpeed(SizeB1024(math.Round(speed))))
		}
	case SizeB1000:
		if format == "" {
			format = "% d"
		}
		return func(speed float64) string {
			return fmt.Sprintf(format, FmtAsSpeed(SizeB1000(math.Round(speed))))
		}
	default:
		if format == "" {
			format = "%f"
		}
		return func(speed float64) string {
			return fmt.Sprintf(format, speed)
		}
	}
}
