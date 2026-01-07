//go:generate go tool stringer -type=SizeB1024 -trimprefix=_i
//go:generate go tool stringer -type=SizeB1000 -trimprefix=_

package decor

import (
	"fmt"
	"strconv"
)

var (
	_ fmt.Formatter = SizeB1024(0)
	_ fmt.Stringer  = SizeB1024(0)
	_ fmt.Formatter = SizeB1000(0)
	_ fmt.Stringer  = SizeB1000(0)
)

const (
	_ib   SizeB1024 = iota + 1
	_iKiB SizeB1024 = 1 << (iota * 10)
	_iMiB
	_iGiB
	_iTiB
)

// SizeB1024 named type, which implements fmt.Formatter interface. It
// adjusts its value according to byte size multiple by 1024 and appends
// appropriate size marker (KiB, MiB, GiB, TiB).
type SizeB1024 int64

func (s SizeB1024) Format(f fmt.State, verb rune) {
	prec := -1
	switch verb {
	case 'f', 'e', 'E':
		prec = 6 // default prec of fmt.Printf("%f|%e|%E")
		fallthrough
	case 'b', 'g', 'G', 'x', 'X':
		if p, ok := f.Precision(); ok {
			prec = p
		}
	default:
		verb, prec = 'f', 0
	}

	var unit SizeB1024
	switch {
	case s < _iKiB:
		unit = _ib
	case s < _iMiB:
		unit = _iKiB
	case s < _iGiB:
		unit = _iMiB
	case s < _iTiB:
		unit = _iGiB
	default:
		unit = _iTiB
	}

	b := strconv.AppendFloat(make([]byte, 0, 24), float64(s)/float64(unit), byte(verb), prec, 64)
	if f.Flag(' ') {
		b = append(b, ' ')
	}
	b = append(b, []byte(unit.String())...)
	_, err := f.Write(b)
	if err != nil {
		panic(err)
	}
}

const (
	_b  SizeB1000 = 1
	_KB SizeB1000 = _b * 1000
	_MB SizeB1000 = _KB * 1000
	_GB SizeB1000 = _MB * 1000
	_TB SizeB1000 = _GB * 1000
)

// SizeB1000 named type, which implements fmt.Formatter interface. It
// adjusts its value according to byte size multiple by 1000 and appends
// appropriate size marker (KB, MB, GB, TB).
type SizeB1000 int64

func (s SizeB1000) Format(f fmt.State, verb rune) {
	prec := -1
	switch verb {
	case 'f', 'e', 'E':
		prec = 6 // default prec of fmt.Printf("%f|%e|%E")
		fallthrough
	case 'b', 'g', 'G', 'x', 'X':
		if p, ok := f.Precision(); ok {
			prec = p
		}
	default:
		verb, prec = 'f', 0
	}

	var unit SizeB1000
	switch {
	case s < _KB:
		unit = _b
	case s < _MB:
		unit = _KB
	case s < _GB:
		unit = _MB
	case s < _TB:
		unit = _GB
	default:
		unit = _TB
	}

	b := strconv.AppendFloat(make([]byte, 0, 24), float64(s)/float64(unit), byte(verb), prec, 64)
	if f.Flag(' ') {
		b = append(b, ' ')
	}
	b = append(b, []byte(unit.String())...)
	_, err := f.Write(b)
	if err != nil {
		panic(err)
	}
}
