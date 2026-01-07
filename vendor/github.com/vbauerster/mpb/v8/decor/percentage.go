package decor

import (
	"fmt"
	"strconv"

	"github.com/vbauerster/mpb/v8/internal"
)

var _ fmt.Formatter = percentageType(0)

type percentageType float64

func (s percentageType) Format(st fmt.State, verb rune) {
	prec := -1
	switch verb {
	case 'f', 'e', 'E':
		prec = 6 // default prec of fmt.Printf("%f|%e|%E")
		fallthrough
	case 'b', 'g', 'G', 'x', 'X':
		if p, ok := st.Precision(); ok {
			prec = p
		}
	default:
		verb, prec = 'f', 0
	}

	b := strconv.AppendFloat(make([]byte, 0, 16), float64(s), byte(verb), prec, 64)
	if st.Flag(' ') {
		b = append(b, ' ', '%')
	} else {
		b = append(b, '%')
	}
	_, err := st.Write(b)
	if err != nil {
		panic(err)
	}
}

// Percentage returns percentage decorator. It's a wrapper of NewPercentage.
func Percentage(wcc ...WC) Decorator {
	return NewPercentage("% d", wcc...)
}

// NewPercentage percentage decorator with custom format string.
//
//	`format` printf compatible verb
//
//	`wcc` optional WC config
//
// format examples:
//
//	format="%d"    output: "1%"
//	format="% d"   output: "1 %"
//	format="%.1f"  output: "1.0%"
//	format="% .1f" output: "1.0 %"
//	format="%f"    output: "1.000000%"
//	format="% f"   output: "1.000000 %"
func NewPercentage(format string, wcc ...WC) Decorator {
	if format == "" {
		format = "% d"
	}
	f := func(s Statistics) string {
		p := internal.Percentage(uint(s.Total), uint(s.Current), 100)
		return fmt.Sprintf(format, percentageType(p))
	}
	return Any(f, wcc...)
}
