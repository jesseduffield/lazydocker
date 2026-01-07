package decor

var defaultSpinnerStyle = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner returns spinner decorator.
//
//	`frames` spinner frames, if nil or len==0, default is used
//
//	`wcc` optional WC config
func Spinner(frames []string, wcc ...WC) Decorator {
	if len(frames) == 0 {
		frames = defaultSpinnerStyle[:]
	}
	var count uint
	f := func(s Statistics) string {
		frame := frames[count%uint(len(frames))]
		count++
		return frame
	}
	return Any(f, wcc...)
}
