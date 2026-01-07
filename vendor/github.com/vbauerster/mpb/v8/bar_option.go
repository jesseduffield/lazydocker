package mpb

import (
	"bytes"
	"io"

	"github.com/vbauerster/mpb/v8/decor"
)

// BarOption is a func option to alter default behavior of a bar.
type BarOption func(*bState)

// PrependDecorators let you inject decorators to the bar's left side.
func PrependDecorators(decorators ...decor.Decorator) BarOption {
	var group []decor.Decorator
	for _, decorator := range decorators {
		if decorator != nil {
			group = append(group, decorator)
		}
	}
	return func(s *bState) {
		s.decorGroups[0] = group
	}
}

// AppendDecorators let you inject decorators to the bar's right side.
func AppendDecorators(decorators ...decor.Decorator) BarOption {
	var group []decor.Decorator
	for _, decorator := range decorators {
		if decorator != nil {
			group = append(group, decorator)
		}
	}
	return func(s *bState) {
		s.decorGroups[1] = group
	}
}

// BarID sets bar id.
func BarID(id int) BarOption {
	return func(s *bState) {
		s.id = id
	}
}

// BarWidth sets bar width independent of the container.
func BarWidth(width int) BarOption {
	return func(s *bState) {
		s.reqWidth = width
	}
}

// BarQueueAfter puts this (being constructed) bar into the queue.
// BarPriority will be inherited from the argument bar.
// When argument bar completes or aborts queued bar replaces its place.
func BarQueueAfter(bar *Bar) BarOption {
	return func(s *bState) {
		s.waitBar = bar
	}
}

// BarRemoveOnComplete removes both bar's filler and its decorators
// on complete event.
func BarRemoveOnComplete() BarOption {
	return func(s *bState) {
		s.rmOnComplete = true
	}
}

// BarFillerClearOnComplete clears bar's filler on complete event.
// It's shortcut for BarFillerOnComplete("").
func BarFillerClearOnComplete() BarOption {
	return BarFillerOnComplete("")
}

// BarFillerOnComplete replaces bar's filler with message, on complete event.
func BarFillerOnComplete(message string) BarOption {
	return BarFillerMiddleware(func(base BarFiller) BarFiller {
		return BarFillerFunc(func(w io.Writer, st decor.Statistics) error {
			if st.Completed {
				_, err := io.WriteString(w, message)
				return err
			}
			return base.Fill(w, st)
		})
	})
}

// BarFillerClearOnAbort clears bar's filler on abort event.
// It's shortcut for BarFillerOnAbort("").
func BarFillerClearOnAbort() BarOption {
	return BarFillerOnAbort("")
}

// BarFillerOnAbort replaces bar's filler with message, on abort event.
func BarFillerOnAbort(message string) BarOption {
	return BarFillerMiddleware(func(base BarFiller) BarFiller {
		return BarFillerFunc(func(w io.Writer, st decor.Statistics) error {
			if st.Aborted {
				_, err := io.WriteString(w, message)
				return err
			}
			return base.Fill(w, st)
		})
	})
}

// BarFillerMiddleware provides a way to augment the underlying BarFiller.
func BarFillerMiddleware(middle func(BarFiller) BarFiller) BarOption {
	if middle == nil {
		return nil
	}
	return func(s *bState) {
		s.filler = middle(s.filler)
	}
}

// BarPriority sets bar's priority. Zero is highest priority, i.e. bar
// will be on top. This option isn't effective with `BarQueueAfter` option.
func BarPriority(priority int) BarOption {
	return func(s *bState) {
		s.priority = priority
	}
}

// BarExtender extends bar with arbitrary lines. Provided BarFiller will be
// called at each render/flush cycle. Any lines written to the underlying
// io.Writer will extend the bar either in above (rev = true) or below
// (rev = false) direction.
func BarExtender(filler BarFiller, rev bool) BarOption {
	if filler == nil {
		return nil
	}
	if f, ok := filler.(BarFillerFunc); ok && f == nil {
		return nil
	}
	fn := makeExtenderFunc(filler, rev)
	return func(s *bState) {
		s.extender = fn
	}
}

func makeExtenderFunc(filler BarFiller, rev bool) extenderFunc {
	buf := new(bytes.Buffer)
	base := func(stat decor.Statistics, rows ...io.Reader) ([]io.Reader, error) {
		err := filler.Fill(buf, stat)
		if err != nil {
			buf.Reset()
			return rows, err
		}
		for {
			line, err := buf.ReadBytes('\n')
			if err != nil {
				buf.Reset()
				break
			}
			rows = append(rows, bytes.NewReader(line))
		}
		return rows, err
	}
	if !rev {
		return base
	}
	return func(stat decor.Statistics, rows ...io.Reader) ([]io.Reader, error) {
		rows, err := base(stat, rows...)
		if err != nil {
			return rows, err
		}
		for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
			rows[left], rows[right] = rows[right], rows[left]
		}
		return rows, err
	}
}

// BarFillerTrim removes leading and trailing space around the underlying BarFiller.
func BarFillerTrim() BarOption {
	return func(s *bState) {
		s.trimSpace = true
	}
}

// BarNoPop disables bar pop out of container. Effective when
// PopCompletedMode of container is enabled.
func BarNoPop() BarOption {
	return func(s *bState) {
		s.noPop = true
	}
}

// BarOptional will return provided option only when cond is true.
func BarOptional(option BarOption, cond bool) BarOption {
	if cond {
		return option
	}
	return nil
}

// BarOptOn will return provided option only when predicate evaluates to true.
func BarOptOn(option BarOption, predicate func() bool) BarOption {
	if predicate() {
		return option
	}
	return nil
}

// BarFuncOptional will call option and return its value only when cond is true.
func BarFuncOptional(option func() BarOption, cond bool) BarOption {
	if cond {
		return option()
	}
	return nil
}

// BarFuncOptOn will call option and return its value only when predicate evaluates to true.
func BarFuncOptOn(option func() BarOption, predicate func() bool) BarOption {
	if predicate() {
		return option()
	}
	return nil
}
