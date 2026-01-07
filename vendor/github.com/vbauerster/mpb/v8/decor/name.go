package decor

// Name decorator displays text that is set once and can't be changed
// during decorator's lifetime.
//
//	`str` string to display
//
//	`wcc` optional WC config
func Name(str string, wcc ...WC) Decorator {
	return Any(func(Statistics) string { return str }, wcc...)
}
