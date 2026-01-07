package mpb

import (
	"io"

	"github.com/vbauerster/mpb/v8/decor"
)

// barFillerBuilderFunc is function type adapter to convert compatible
// function into BarFillerBuilder interface.
type barFillerBuilderFunc func() BarFiller

func (f barFillerBuilderFunc) Build() BarFiller {
	return f()
}

// NopStyle provides BarFillerBuilder which builds NOP BarFiller.
func NopStyle() BarFillerBuilder {
	return barFillerBuilderFunc(func() BarFiller {
		return BarFillerFunc(func(io.Writer, decor.Statistics) error {
			return nil
		})
	})
}
