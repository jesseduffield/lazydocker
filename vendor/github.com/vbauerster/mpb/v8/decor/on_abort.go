package decor

var (
	_ Decorator = onAbortWrapper{}
	_ Wrapper   = onAbortWrapper{}
	_ Decorator = onAbortMetaWrapper{}
	_ Wrapper   = onAbortMetaWrapper{}
)

// OnAbort wrap decorator.
// Displays provided message on abort event.
// Has no effect if bar.Abort(true) is called.
//
//	`decorator` Decorator to wrap
//	`message` message to display
func OnAbort(decorator Decorator, message string) Decorator {
	if decorator == nil {
		return nil
	}
	return onAbortWrapper{decorator, message}
}

type onAbortWrapper struct {
	Decorator
	msg string
}

func (d onAbortWrapper) Decor(s Statistics) (string, int) {
	if s.Aborted {
		return d.Format(d.msg)
	}
	return d.Decorator.Decor(s)
}

func (d onAbortWrapper) Unwrap() Decorator {
	return d.Decorator
}

// OnAbortMeta wrap decorator.
// Provided fn is supposed to wrap output of given decorator
// with meta information like ANSI escape codes for example.
// Primary usage intention is to set SGR display attributes.
//
//	`decorator` Decorator to wrap
//	`fn` func to apply meta information
func OnAbortMeta(decorator Decorator, fn func(string) string) Decorator {
	if decorator == nil {
		return nil
	}
	return onAbortMetaWrapper{decorator, fn}
}

type onAbortMetaWrapper struct {
	Decorator
	fn func(string) string
}

func (d onAbortMetaWrapper) Decor(s Statistics) (string, int) {
	if s.Aborted {
		str, width := d.Decorator.Decor(s)
		return d.fn(str), width
	}
	return d.Decorator.Decor(s)
}

func (d onAbortMetaWrapper) Unwrap() Decorator {
	return d.Decorator
}
