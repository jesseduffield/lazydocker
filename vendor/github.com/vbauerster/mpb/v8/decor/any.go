package decor

var _ Decorator = any{}

// Any decorator.
// Converts DecorFunc into Decorator.
//
//	`fn` DecorFunc callback
//	`wcc` optional WC config
func Any(fn DecorFunc, wcc ...WC) Decorator {
	return any{initWC(wcc...), fn}
}

type any struct {
	WC
	fn DecorFunc
}

func (d any) Decor(s Statistics) (string, int) {
	return d.Format(d.fn(s))
}
