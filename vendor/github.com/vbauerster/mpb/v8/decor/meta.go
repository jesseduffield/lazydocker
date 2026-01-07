package decor

var (
	_ Decorator = metaWrapper{}
	_ Wrapper   = metaWrapper{}
)

// Meta wrap decorator.
// Provided fn is supposed to wrap output of given decorator
// with meta information like ANSI escape codes for example.
// Primary usage intention is to set SGR display attributes.
//
//	`decorator` Decorator to wrap
//	`fn` func to apply meta information
func Meta(decorator Decorator, fn func(string) string) Decorator {
	if decorator == nil {
		return nil
	}
	return metaWrapper{decorator, fn}
}

type metaWrapper struct {
	Decorator
	fn func(string) string
}

func (d metaWrapper) Decor(s Statistics) (string, int) {
	str, width := d.Decorator.Decor(s)
	return d.fn(str), width
}

func (d metaWrapper) Unwrap() Decorator {
	return d.Decorator
}
