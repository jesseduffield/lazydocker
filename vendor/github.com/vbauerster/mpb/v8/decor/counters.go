package decor

import (
	"fmt"
)

// CountersNoUnit is a wrapper around Counters with no unit param.
func CountersNoUnit(pairFmt string, wcc ...WC) Decorator {
	return Counters(0, pairFmt, wcc...)
}

// CountersKibiByte is a wrapper around Counters with predefined unit
// as SizeB1024(0).
func CountersKibiByte(pairFmt string, wcc ...WC) Decorator {
	return Counters(SizeB1024(0), pairFmt, wcc...)
}

// CountersKiloByte is a wrapper around Counters with predefined unit
// as SizeB1000(0).
func CountersKiloByte(pairFmt string, wcc ...WC) Decorator {
	return Counters(SizeB1000(0), pairFmt, wcc...)
}

// Counters decorator with dynamic unit measure adjustment.
//
//	`unit` one of [0|SizeB1024(0)|SizeB1000(0)]
//
//	`pairFmt` printf compatible verbs for current and total
//
//	`wcc` optional WC config
//
// pairFmt example if unit=SizeB1000(0):
//
//	pairFmt="%d / %d"       output: "1MB / 12MB"
//	pairFmt="% d / % d"     output: "1 MB / 12 MB"
//	pairFmt="%.1f / %.1f"   output: "1.0MB / 12.0MB"
//	pairFmt="% .1f / % .1f" output: "1.0 MB / 12.0 MB"
//	pairFmt="%f / %f"       output: "1.000000MB / 12.000000MB"
//	pairFmt="% f / % f"     output: "1.000000 MB / 12.000000 MB"
func Counters(unit interface{}, pairFmt string, wcc ...WC) Decorator {
	producer := func() DecorFunc {
		switch unit.(type) {
		case SizeB1024:
			if pairFmt == "" {
				pairFmt = "% d / % d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(pairFmt, SizeB1024(s.Current), SizeB1024(s.Total))
			}
		case SizeB1000:
			if pairFmt == "" {
				pairFmt = "% d / % d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(pairFmt, SizeB1000(s.Current), SizeB1000(s.Total))
			}
		default:
			if pairFmt == "" {
				pairFmt = "%d / %d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(pairFmt, s.Current, s.Total)
			}
		}
	}
	return Any(producer(), wcc...)
}

// TotalNoUnit is a wrapper around Total with no unit param.
func TotalNoUnit(format string, wcc ...WC) Decorator {
	return Total(0, format, wcc...)
}

// TotalKibiByte is a wrapper around Total with predefined unit
// as SizeB1024(0).
func TotalKibiByte(format string, wcc ...WC) Decorator {
	return Total(SizeB1024(0), format, wcc...)
}

// TotalKiloByte is a wrapper around Total with predefined unit
// as SizeB1000(0).
func TotalKiloByte(format string, wcc ...WC) Decorator {
	return Total(SizeB1000(0), format, wcc...)
}

// Total decorator with dynamic unit measure adjustment.
//
//	`unit` one of [0|SizeB1024(0)|SizeB1000(0)]
//
//	`format` printf compatible verb for Total
//
//	`wcc` optional WC config
//
// format example if unit=SizeB1024(0):
//
//	format="%d"    output: "12MiB"
//	format="% d"   output: "12 MiB"
//	format="%.1f"  output: "12.0MiB"
//	format="% .1f" output: "12.0 MiB"
//	format="%f"    output: "12.000000MiB"
//	format="% f"   output: "12.000000 MiB"
func Total(unit interface{}, format string, wcc ...WC) Decorator {
	producer := func() DecorFunc {
		switch unit.(type) {
		case SizeB1024:
			if format == "" {
				format = "% d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, SizeB1024(s.Total))
			}
		case SizeB1000:
			if format == "" {
				format = "% d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, SizeB1000(s.Total))
			}
		default:
			if format == "" {
				format = "%d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, s.Total)
			}
		}
	}
	return Any(producer(), wcc...)
}

// CurrentNoUnit is a wrapper around Current with no unit param.
func CurrentNoUnit(format string, wcc ...WC) Decorator {
	return Current(0, format, wcc...)
}

// CurrentKibiByte is a wrapper around Current with predefined unit
// as SizeB1024(0).
func CurrentKibiByte(format string, wcc ...WC) Decorator {
	return Current(SizeB1024(0), format, wcc...)
}

// CurrentKiloByte is a wrapper around Current with predefined unit
// as SizeB1000(0).
func CurrentKiloByte(format string, wcc ...WC) Decorator {
	return Current(SizeB1000(0), format, wcc...)
}

// Current decorator with dynamic unit measure adjustment.
//
//	`unit` one of [0|SizeB1024(0)|SizeB1000(0)]
//
//	`format` printf compatible verb for Current
//
//	`wcc` optional WC config
//
// format example if unit=SizeB1024(0):
//
//	format="%d"    output: "12MiB"
//	format="% d"   output: "12 MiB"
//	format="%.1f"  output: "12.0MiB"
//	format="% .1f" output: "12.0 MiB"
//	format="%f"    output: "12.000000MiB"
//	format="% f"   output: "12.000000 MiB"
func Current(unit interface{}, format string, wcc ...WC) Decorator {
	producer := func() DecorFunc {
		switch unit.(type) {
		case SizeB1024:
			if format == "" {
				format = "% d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, SizeB1024(s.Current))
			}
		case SizeB1000:
			if format == "" {
				format = "% d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, SizeB1000(s.Current))
			}
		default:
			if format == "" {
				format = "%d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, s.Current)
			}
		}
	}
	return Any(producer(), wcc...)
}

// InvertedCurrentNoUnit is a wrapper around InvertedCurrent with no unit param.
func InvertedCurrentNoUnit(format string, wcc ...WC) Decorator {
	return InvertedCurrent(0, format, wcc...)
}

// InvertedCurrentKibiByte is a wrapper around InvertedCurrent with predefined unit
// as SizeB1024(0).
func InvertedCurrentKibiByte(format string, wcc ...WC) Decorator {
	return InvertedCurrent(SizeB1024(0), format, wcc...)
}

// InvertedCurrentKiloByte is a wrapper around InvertedCurrent with predefined unit
// as SizeB1000(0).
func InvertedCurrentKiloByte(format string, wcc ...WC) Decorator {
	return InvertedCurrent(SizeB1000(0), format, wcc...)
}

// InvertedCurrent decorator with dynamic unit measure adjustment.
//
//	`unit` one of [0|SizeB1024(0)|SizeB1000(0)]
//
//	`format` printf compatible verb for InvertedCurrent
//
//	`wcc` optional WC config
//
// format example if unit=SizeB1024(0):
//
//	format="%d"    output: "12MiB"
//	format="% d"   output: "12 MiB"
//	format="%.1f"  output: "12.0MiB"
//	format="% .1f" output: "12.0 MiB"
//	format="%f"    output: "12.000000MiB"
//	format="% f"   output: "12.000000 MiB"
func InvertedCurrent(unit interface{}, format string, wcc ...WC) Decorator {
	producer := func() DecorFunc {
		switch unit.(type) {
		case SizeB1024:
			if format == "" {
				format = "% d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, SizeB1024(s.Total-s.Current))
			}
		case SizeB1000:
			if format == "" {
				format = "% d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, SizeB1000(s.Total-s.Current))
			}
		default:
			if format == "" {
				format = "%d"
			}
			return func(s Statistics) string {
				return fmt.Sprintf(format, s.Total-s.Current)
			}
		}
	}
	return Any(producer(), wcc...)
}
