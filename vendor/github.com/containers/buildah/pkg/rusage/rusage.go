package rusage

import (
	"fmt"
	"time"

	units "github.com/docker/go-units"
)

// Rusage is a subset of a Unix-style resource usage counter for the current
// process and its children.  The counters are always 0 on platforms where the
// system call is not available (i.e., systems where getrusage() doesn't
// exist).
type Rusage struct {
	Date              time.Time
	Elapsed           time.Duration
	Utime, Stime      time.Duration
	Inblock, Outblock int64
}

// FormatDiff formats the result of rusage.Rusage.Subtract() for logging.
func FormatDiff(diff Rusage) string {
	return fmt.Sprintf("%s(system) %s(user) %s(elapsed) %s input %s output", diff.Stime.Round(time.Millisecond), diff.Utime.Round(time.Millisecond), diff.Elapsed.Round(time.Millisecond), units.HumanSize(float64(diff.Inblock*512)), units.HumanSize(float64(diff.Outblock*512)))
}

// Subtract subtracts the items in delta from r, and returns the difference.
// The Date field is zeroed for easier comparison with the zero value for the
// Rusage type.
func (r Rusage) Subtract(baseline Rusage) Rusage {
	return Rusage{
		Elapsed:  r.Date.Sub(baseline.Date),
		Utime:    r.Utime - baseline.Utime,
		Stime:    r.Stime - baseline.Stime,
		Inblock:  r.Inblock - baseline.Inblock,
		Outblock: r.Outblock - baseline.Outblock,
	}
}

// Get returns the counters for the current process and its children,
// subtracting any values in the passed in "since" value, or an error.
// The Elapsed field will always be set to zero.
func Get() (Rusage, error) {
	counters, err := get()
	if err != nil {
		return Rusage{}, err
	}
	return counters, nil
}
