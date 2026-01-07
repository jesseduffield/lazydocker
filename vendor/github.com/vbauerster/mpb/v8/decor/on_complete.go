package decor

var (
	_ Decorator = onCompleteWrapper{}
	_ Wrapper   = onCompleteWrapper{}
	_ Decorator = onCompleteMetaWrapper{}
	_ Wrapper   = onCompleteMetaWrapper{}
)

// OnComplete wrap decorator.
// Displays provided message on complete event.
//
//	`decorator` Decorator to wrap
//	`message` message to display
func OnComplete(decorator Decorator, message string) Decorator {
	if decorator == nil {
		return nil
	}
	return onCompleteWrapper{decorator, message}
}

type onCompleteWrapper struct {
	Decorator
	msg string
}

func (d onCompleteWrapper) Decor(s Statistics) (string, int) {
	if s.Completed {
		return d.Format(d.msg)
	}
	return d.Decorator.Decor(s)
}

func (d onCompleteWrapper) Unwrap() Decorator {
	return d.Decorator
}

// OnCompleteMeta wrap decorator.
// Provided fn is supposed to wrap output of given decorator
// with meta information like ANSI escape codes for example.
// Primary usage intention is to set SGR display attributes.
//
//	`decorator` Decorator to wrap
//	`fn` func to apply meta information
func OnCompleteMeta(decorator Decorator, fn func(string) string) Decorator {
	if decorator == nil {
		return nil
	}
	return onCompleteMetaWrapper{decorator, fn}
}

type onCompleteMetaWrapper struct {
	Decorator
	fn func(string) string
}

func (d onCompleteMetaWrapper) Decor(s Statistics) (string, int) {
	if s.Completed {
		str, width := d.Decorator.Decor(s)
		return d.fn(str), width
	}
	return d.Decorator.Decor(s)
}

func (d onCompleteMetaWrapper) Unwrap() Decorator {
	return d.Decorator
}
