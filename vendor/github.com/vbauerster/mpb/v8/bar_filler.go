package mpb

import (
	"io"

	"github.com/vbauerster/mpb/v8/decor"
)

// BarFiller interface.
// Bar (without decorators) renders itself by calling BarFiller's Fill method.
type BarFiller interface {
	Fill(io.Writer, decor.Statistics) error
}

// BarFillerBuilder interface.
// Default implementations are:
//
//	BarStyle()
//	SpinnerStyle()
//	NopStyle()
type BarFillerBuilder interface {
	Build() BarFiller
}

// BarFillerFunc is function type adapter to convert compatible function
// into BarFiller interface.
type BarFillerFunc func(io.Writer, decor.Statistics) error

func (f BarFillerFunc) Fill(w io.Writer, stat decor.Statistics) error {
	return f(w, stat)
}
