// SPDX-License-Identifier: BSD-3-Clause
package common

import "fmt"

const (
	maxWarnings             = 100 // An arbitrary limit to avoid excessive memory usage, it has no sense to store hundreds of errors
	tooManyErrorsMessage    = "too many errors reported, next errors were discarded"
	numberOfWarningsMessage = "Number of warnings:"
)

type Warnings struct {
	List          []error
	tooManyErrors bool
	Verbose       bool
}

func (w *Warnings) Add(err error) {
	if len(w.List) >= maxWarnings {
		w.tooManyErrors = true
		return
	}
	w.List = append(w.List, err)
}

func (w *Warnings) Reference() error {
	if len(w.List) > 0 {
		return w
	}
	return nil
}

func (w *Warnings) Error() string {
	if w.Verbose {
		str := ""
		for i, e := range w.List {
			str += fmt.Sprintf("\tError %d: %s\n", i, e.Error())
		}
		if w.tooManyErrors {
			str += fmt.Sprintf("\t%s\n", tooManyErrorsMessage)
		}
		return str
	}
	if w.tooManyErrors {
		return fmt.Sprintf("%s > %v - %s", numberOfWarningsMessage, maxWarnings, tooManyErrorsMessage)
	}
	return fmt.Sprintf("%s %v", numberOfWarningsMessage, len(w.List))
}
