package decor

import (
	"fmt"
	"time"

	"github.com/mattn/go-runewidth"
)

const (
	// DindentRight sets indentation from right to left.
	//
	//	|foo   |b     | DindentRight is set
	//	|   foo|     b| DindentRight is not set
	DindentRight = 1 << iota

	// DextraSpace bit adds extra indentation space.
	DextraSpace

	// DSyncWidth bit enables same column width synchronization.
	// Effective with multiple bars only.
	DSyncWidth

	// DSyncWidthR is shortcut for DSyncWidth|DindentRight
	DSyncWidthR = DSyncWidth | DindentRight

	// DSyncSpace is shortcut for DSyncWidth|DextraSpace
	DSyncSpace = DSyncWidth | DextraSpace

	// DSyncSpaceR is shortcut for DSyncWidth|DextraSpace|DindentRight
	DSyncSpaceR = DSyncWidth | DextraSpace | DindentRight
)

// TimeStyle enum.
type TimeStyle int

// TimeStyle kinds.
const (
	ET_STYLE_GO TimeStyle = iota
	ET_STYLE_HHMMSS
	ET_STYLE_HHMM
	ET_STYLE_MMSS
)

// Statistics contains fields which are necessary for implementing
// `decor.Decorator` and `mpb.BarFiller` interfaces.
type Statistics struct {
	AvailableWidth int // calculated width initially equal to terminal width
	RequestedWidth int // width set by `mpb.WithWidth`
	ID             int
	Total          int64
	Current        int64
	Refill         int64
	Completed      bool
	Aborted        bool
}

// Decorator interface.
// Most of the time there is no need to implement this interface
// manually, as decor package already provides a wide range of decorators
// which implement this interface. If however built-in decorators don't
// meet your needs, you're free to implement your own one by implementing
// this particular interface. The easy way to go is to convert a
// `DecorFunc` into a `Decorator` interface by using provided
// `func Any(DecorFunc, ...WC) Decorator`.
type Decorator interface {
	Synchronizer
	Formatter
	Decor(Statistics) (str string, viewWidth int)
}

// DecorFunc func type.
// To be used with `func Any(DecorFunc, ...WC) Decorator`.
type DecorFunc func(Statistics) string

// Synchronizer interface.
// All decorators implement this interface implicitly. Its Sync
// method exposes width sync channel, if DSyncWidth bit is set.
type Synchronizer interface {
	Sync() (chan int, bool)
}

// Formatter interface.
// Format method needs to be called from within Decorator.Decor method
// in order to format string according to decor.WC settings.
// No need to implement manually as long as decor.WC is embedded.
type Formatter interface {
	Format(string) (_ string, width int)
}

// Wrapper interface.
// If you're implementing custom Decorator by wrapping a built-in one,
// it is necessary to implement this interface to retain functionality
// of built-in Decorator.
type Wrapper interface {
	Unwrap() Decorator
}

// EwmaDecorator interface.
// EWMA based decorators should implement this one.
type EwmaDecorator interface {
	EwmaUpdate(int64, time.Duration)
}

// AverageDecorator interface.
// Average decorators should implement this interface to provide start
// time adjustment facility, for resume-able tasks.
type AverageDecorator interface {
	AverageAdjust(time.Time)
}

// ShutdownListener interface.
// If decorator needs to be notified once upon bar shutdown event, so
// this is the right interface to implement.
type ShutdownListener interface {
	OnShutdown()
}

// Global convenience instances of WC with sync width bit set.
// To be used with multiple bars only, i.e. not effective for single bar usage.
var (
	WCSyncWidth  = WC{C: DSyncWidth}
	WCSyncWidthR = WC{C: DSyncWidthR}
	WCSyncSpace  = WC{C: DSyncSpace}
	WCSyncSpaceR = WC{C: DSyncSpaceR}
)

// WC is a struct with two public fields W and C, both of int type.
// W represents width and C represents bit set of width related config.
// A decorator should embed WC, to enable width synchronization.
type WC struct {
	W     int
	C     int
	fill  func(s string, w int) string
	wsync chan int
}

// Format should be called by any Decorator implementation.
// Returns formatted string and its view (visual) width.
func (wc WC) Format(str string) (string, int) {
	width := runewidth.StringWidth(str)
	if wc.W > width {
		width = wc.W
	} else if (wc.C & DextraSpace) != 0 {
		width++
	}
	if (wc.C & DSyncWidth) != 0 {
		wc.wsync <- width
		width = <-wc.wsync
	}
	return wc.fill(str, width), width
}

// Init initializes width related config.
func (wc *WC) Init() WC {
	if (wc.C & DindentRight) != 0 {
		wc.fill = runewidth.FillRight
	} else {
		wc.fill = runewidth.FillLeft
	}
	if (wc.C & DSyncWidth) != 0 {
		// it's deliberate choice to override wsync on each Init() call,
		// this way globals like WCSyncSpace can be reused
		wc.wsync = make(chan int)
	}
	return *wc
}

// Sync is implementation of Synchronizer interface.
func (wc WC) Sync() (chan int, bool) {
	if (wc.C&DSyncWidth) != 0 && wc.wsync == nil {
		panic(fmt.Sprintf("%T is not initialized", wc))
	}
	return wc.wsync, (wc.C & DSyncWidth) != 0
}

func initWC(wcc ...WC) WC {
	var wc WC
	for _, nwc := range wcc {
		wc = nwc
	}
	return wc.Init()
}
