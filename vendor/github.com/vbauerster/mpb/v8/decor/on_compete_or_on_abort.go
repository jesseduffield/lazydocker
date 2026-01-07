package decor

// OnCompleteOrOnAbort wrap decorator.
// Displays provided message on complete or on abort event.
//
//	`decorator` Decorator to wrap
//	`message` message to display
func OnCompleteOrOnAbort(decorator Decorator, message string) Decorator {
	return OnComplete(OnAbort(decorator, message), message)
}

// OnCompleteMetaOrOnAbortMeta wrap decorator.
// Provided fn is supposed to wrap output of given decorator
// with meta information like ANSI escape codes for example.
// Primary usage intention is to set SGR display attributes.
//
//	`decorator` Decorator to wrap
//	`fn` func to apply meta information
func OnCompleteMetaOrOnAbortMeta(decorator Decorator, fn func(string) string) Decorator {
	return OnCompleteMeta(OnAbortMeta(decorator, fn), fn)
}
