package decor

// OnCondition applies decorator only if a condition is true.
//
//	`decorator` Decorator
//
//	`cond` bool
func OnCondition(decorator Decorator, cond bool) Decorator {
	return Conditional(cond, decorator, nil)
}

// OnPredicate applies decorator only if a predicate evaluates to true.
//
//	`decorator` Decorator
//
//	`predicate` func() bool
func OnPredicate(decorator Decorator, predicate func() bool) Decorator {
	return Predicative(predicate, decorator, nil)
}

// Conditional returns decorator `a` if condition is true, otherwise
// decorator `b`.
//
//	`cond` bool
//
//	`a` Decorator
//
//	`b` Decorator
func Conditional(cond bool, a, b Decorator) Decorator {
	if cond {
		return a
	} else {
		return b
	}
}

// Predicative returns decorator `a` if predicate evaluates to true,
// otherwise decorator `b`.
//
//	`predicate` func() bool
//
//	`a` Decorator
//
//	`b` Decorator
func Predicative(predicate func() bool, a, b Decorator) Decorator {
	if predicate() {
		return a
	} else {
		return b
	}
}
