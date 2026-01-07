// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package intervalset provides an abtraction for dealing with sets of
// 1-dimensional spans, such as sets of time ranges. The Set type provides set
// arithmetic and enumeration methods based on an Interval interface.
//
// DISCLAIMER: This library is not yet stable, so expect breaking changes.
package intervalset

import (
	"fmt"
	"sort"
	"strings"
)

// Interval is the interface for a continuous or discrete span. The interval is
// assumed to be inclusive of the starting point and exclusive of the ending
// point.
//
// All methods in the interface are non-destructive: Calls to the methods should
// not modify the interval. Furthermore, the implementation assumes an interval
// will not be mutated by user code, either.
type Interval interface {
	// Intersect returns the intersection of an interval with another
	// interval. The function may panic if the other interval is incompatible.
	Intersect(Interval) Interval

	// Before returns true if the interval is completely before another interval.
	Before(Interval) bool

	// IsZero returns true for the zero value of an interval.
	IsZero() bool

	// Bisect returns two intervals, one on the lower side of x and one on the
	// upper side of x, corresponding to the subtraction of x from the original
	// interval. The returned intervals are always within the range of the
	// original interval.
	Bisect(x Interval) (Interval, Interval)

	// Adjoin returns the union of two intervals, if the intervals are exactly
	// adjacent, or the zero interval if they are not.
	Adjoin(Interval) Interval

	// Encompass returns an interval that covers the exact extents of two
	// intervals.
	Encompass(Interval) Interval
}

// Set is a set of interval objects used for
type Set struct {
	//non-overlapping intervals
	intervals []Interval
	// factory is needed when the extents of the empty set are needed.
	factory intervalFactory
}

// SetInput is an interface implemented by Set and ImmutableSet. It is used when
// one of these types type must take a set as an argument.
type SetInput interface {
	// Extent returns the Interval defined by the minimum and maximum values of
	// the set.
	Extent() Interval

	// IntervalsBetween iterates over the intervals within extents set and calls f
	// with each. If f returns false, iteration ceases.
	//
	// Any interval within the set that overlaps partially with extents is truncated
	// before being passed to f.
	IntervalsBetween(extents Interval, f IntervalReceiver)
}

// NewSet returns a new set given a sorted slice of intervals. This function
// panics if the intervals are not sorted.
func NewSet(intervals []Interval) *Set {
	return NewSetV1(intervals, oldBehaviorFactory.makeZero)
}

// NewSetV1 returns a new set given a sorted slice of intervals. This function
// panics if the intervals are not sorted.
//
// NewSetV1 will be renamed and will replace NewSet in the v1 release.
func NewSetV1(intervals []Interval, makeZero func() Interval) *Set {
	if err := CheckSorted(intervals); err != nil {
		panic(err)
	}
	return &Set{intervals, makeIntervalFactor(makeZero)}
}

// CheckSorted checks that interval[i+1] is not before interval[i] for all
// relevant elements of the input slice. Nil is returned when len(intervals) is
// 0 or 1.
func CheckSorted(intervals []Interval) error {
	for i := 0; i < len(intervals)-1; i++ {
		if !intervals[i].Before(intervals[i+1]) {
			return fmt.Errorf("!intervals[%d].Before(intervals[%d]) for %s, %s", i, i+1, intervals[i], intervals[i+1])
		}
	}
	return nil
}

// Empty returns a new, empty set of intervals.
func Empty() *Set {
	return EmptyV1(oldBehaviorFactory.makeZero)
}

// EmptyV1 returns a new, empty set of intervals using the semantics of the V1
// API, which will require a factory method for construction of an empty interval.
func EmptyV1(makeZero func() Interval) *Set {
	return &Set{nil, makeIntervalFactor(makeZero)}
}

// Copy returns a copy of a set that may be mutated without affecting the original.
func (s *Set) Copy() *Set {
	return &Set{append([]Interval(nil), s.intervals...), s.factory}
}

// String returns a human-friendly representation of the set.
func (s *Set) String() string {
	var strs []string
	for _, x := range s.intervals {
		strs = append(strs, fmt.Sprintf("%s", x))
	}
	return fmt.Sprintf("{%s}", strings.Join(strs, ", "))
}

// Extent returns the Interval defined by the minimum and maximum values of the
// set.
func (s *Set) Extent() Interval {
	if len(s.intervals) == 0 {
		return s.factory.makeZero()
	}
	return s.intervals[0].Encompass(s.intervals[len(s.intervals)-1])
}

// Add adds all the elements of another set to this set.
func (s *Set) Add(b SetInput) {
	// Deal with nil extent. See https://github.com/google/go-intervals/issues/6.
	bExtent := b.Extent()
	if bExtent == nil {
		return // no changes needed
	}

	// Loop through the intervals of x
	b.IntervalsBetween(bExtent, func(x Interval) bool {
		s.insert(x)
		return true
	})
}

// Contains reports whether an interval is entirely contained by the set.
func (s *Set) Contains(ival Interval) bool {
	// Loop through the intervals of x
	next := s.iterator(ival, true)
	for setInterval := next(); setInterval != nil; setInterval = next() {
		left, right := ival.Bisect(setInterval)
		if !left.IsZero() {
			return false
		}
		ival = right
	}
	return ival.IsZero()
}

// adjoinOrAppend adds an interval to the end of intervals unless that value
// directly adjoins the last element of intervals, in which case the last
// element will be replaced by the adjoined interval.
func adjoinOrAppend(intervals []Interval, x Interval) []Interval {
	lastIndex := len(intervals) - 1
	if lastIndex == -1 {
		return append(intervals, x)
	}
	adjoined := intervals[lastIndex].Adjoin(x)
	if adjoined.IsZero() {
		return append(intervals, x)
	}
	intervals[lastIndex] = adjoined
	return intervals
}

func (s *Set) insert(insertion Interval) {
	if s.Contains(insertion) {
		return
	}
	// TODO(reddaly): Something like Java's ArrayList would allow both O(log(n))
	// insertion and O(log(n)) lookup. For now, we have O(log(n)) lookup and O(n)
	// insertion.
	var newIntervals []Interval
	push := func(x Interval) {
		newIntervals = adjoinOrAppend(newIntervals, x)
	}
	inserted := false
	for _, x := range s.intervals {
		if inserted {
			push(x)
			continue
		}
		if insertion.Before(x) {
			push(insertion)
			push(x)
			inserted = true
			continue
		}
		// [===left===)[==x===)[===right===)
		left, right := insertion.Bisect(x)
		if !left.IsZero() {
			push(left)
		}
		push(x)
		// Replace the interval being inserted with the remaining portion of the
		// interval to be inserted.
		if right.IsZero() {
			inserted = true
		} else {
			insertion = right
		}
	}
	if !inserted {
		push(insertion)
	}
	s.intervals = newIntervals
}

// Sub destructively modifies the set by subtracting b.
func (s *Set) Sub(b SetInput) {
	extent := s.Extent()
	// Deal with nil extent. See https://github.com/google/go-intervals/issues/6.
	if extent == nil {
		// Set is already empty, no changes necessary.
		return
	}
	var newIntervals []Interval
	push := func(x Interval) {
		newIntervals = adjoinOrAppend(newIntervals, x)
	}
	nextX := s.iterator(extent, true)
	nextY, cancel := setIntervalIterator(b, extent)
	defer cancel()

	x := nextX()
	y := nextY()
	for x != nil {
		// If y == nil, all of the remaining intervals in A are to the right of B,
		// so just yield them.
		if y == nil {
			push(x)
			x = nextX()
			continue
		}
		// Split x into parts left and right of y.
		// The diagrams below show the bisection results for various situations.
		// if left.IsZero() && !right.IsZero()
		//             xxx
		// y1y1 y2y2 y3  y4y4
		//             xxx
		// or
		//   xxxxxxxxxxxx
		// y1y1 y2y2 y3  y4y4
		//
		// if !left.IsZero() && !right.IsZero()
		//       x1x1x1x1x1
		//         y1  y2
		//
		// if left.IsZero() && right.IsZero()
		//    x1x1x1x1  x2x2x2
		//  y1y1y1y1y1y1y1
		//
		// if !left.IsZero() && right.IsZero()
		//   x1x1  x2
		//     y1y1y1y1
		left, right := x.Bisect(y)

		// If the left side of x is non-zero, it can definitely be pushed to the
		// resulting interval set since no subsequent y value will intersect it.
		// The sequences look something like
		//         x1x1x1x1x1       OR   x1x1x1 x2
		//             y1 y2                       y1y1y1
		// left  = x1x1                  x1x1x1
		// right =       x1x1                            {zero}
		if !left.IsZero() {
			push(left)
		}

		if !right.IsZero() {
			// If the right side of x is non-zero:
			// 1) Right is the remaining portion of x that needs to be pushed.
			x = right
			// 2) It's not possible for current y to intersect it, so advance y. It's
			//    possible nextY() will intersect it, so don't push yet.
			y = nextY()
		} else {
			// There's nothing left of x to push, so advance x.
			x = nextX()
		}
	}

	// Setting s.intervals is the only side effect in this function.
	s.intervals = newIntervals
}

// intersectionIterator returns a function that yields intervals that are
// members of the intersection of s and b, in increasing order.
func (s *Set) intersectionIterator(b SetInput) (iter func() Interval, cancel func()) {
	return intervalMapperToIterator(func(f IntervalReceiver) {
		sExtent, bExtent := s.Extent(), b.Extent()
		// Deal with nil extent. See https://github.com/google/go-intervals/issues/6.
		if sExtent == nil || bExtent == nil {
			// IF either set is already empty, the intersection is empty. This
			// voids a panic below where a valid Interval is needed for each
			// extent.
			return
		}
		nextX := s.iterator(bExtent, true)
		nextY, cancel := setIntervalIterator(b, sExtent)
		defer cancel()

		x := nextX()
		y := nextY()
		// Loop through corresponding intervals of S and B.
		// If y == nil, all of the remaining intervals in S are to the right of B.
		// If x == nil, all of the remaining intervals in B are to the right of S.
		for x != nil && y != nil {
			if x.Before(y) {
				x = nextX()
				continue
			}
			if y.Before(x) {
				y = nextY()
				continue
			}
			xyIntersect := x.Intersect(y)
			if !xyIntersect.IsZero() {
				if !f(xyIntersect) {
					return
				}
				_, right := x.Bisect(y)
				if !right.IsZero() {
					x = right
				} else {
					x = nextX()
				}
			}
		}
	})
}

// Intersect destructively modifies the set by intersectin it with b.
func (s *Set) Intersect(b SetInput) {
	iter, cancel := s.intersectionIterator(b)
	defer cancel()
	var newIntervals []Interval
	for x := iter(); x != nil; x = iter() {
		newIntervals = append(newIntervals, x)
	}
	s.intervals = newIntervals
}

// searchLow returns the first index in s.intervals that is not before x.
func (s *Set) searchLow(x Interval) int {
	return sort.Search(len(s.intervals), func(i int) bool {
		return !s.intervals[i].Before(x)
	})
}

// searchLow returns the index of the first interval in s.intervals that is
// entirely after x.
func (s *Set) searchHigh(x Interval) int {
	return sort.Search(len(s.intervals), func(i int) bool {
		return x.Before(s.intervals[i])
	})
}

// iterator returns a function that yields elements of the set in order.
//
// The function returned will return nil when finished iterating.
func (s *Set) iterator(extents Interval, forward bool) func() Interval {
	low, high := s.searchLow(extents), s.searchHigh(extents)

	i, stride := low, 1
	if !forward {
		i, stride = high-1, -1
	}

	return func() Interval {
		if i < 0 || i >= len(s.intervals) {
			return nil
		}
		x := s.intervals[i]
		i += stride
		return x
	}
}

// IntervalReceiver is a function used for iterating over a set of intervals. It
// takes the start and end times and returns true if the iteration should
// continue.
type IntervalReceiver func(Interval) bool

// IntervalsBetween iterates over the intervals within extents set and calls f
// with each. If f returns false, iteration ceases.
//
// Any interval within the set that overlaps partially with extents is truncated
// before being passed to f.
func (s *Set) IntervalsBetween(extents Interval, f IntervalReceiver) {
	// Begin = first index in s.intervals that is not before extents.
	begin := sort.Search(len(s.intervals), func(i int) bool {
		return !s.intervals[i].Before(extents)
	})

	// TODO(reddaly): Optimize this by performing a binary search for the ending
	// point.
	for _, interval := range s.intervals[begin:] {
		// If the interval is after the extents, there will be no more overlap, so
		// break out of the loop.
		if extents.Before(interval) {
			break
		}
		portionOfInterval := extents.Intersect(interval)
		if portionOfInterval.IsZero() {
			continue
		}

		if !f(portionOfInterval) {
			return
		}
	}
}

// Intervals iterates over all the intervals within the set and calls f with
// each one. If f returns false, iteration ceases.
func (s *Set) Intervals(f IntervalReceiver) {
	for _, interval := range s.intervals {
		if !f(interval) {
			return
		}
	}
}

// AllIntervals returns an ordered slice of all the intervals in the set.
func (s *Set) AllIntervals() []Interval {
	return append(make([]Interval, 0, len(s.intervals)), s.intervals...)
}

// ImmutableSet returns an immutable copy of this set.
func (s *Set) ImmutableSet() *ImmutableSet {
	return NewImmutableSet(s.AllIntervals())
}

// mapFn reports true if an iteration should continue. It is called on values of
// a collection.
type mapFn func(interface{}) bool

// mapFn calls mapFn for each member of a collection.
type mapperFn func(mapFn)

// iteratorFn returns the next item in an iteration or the zero value. The
// second return value indicates whether the first return value is a member of
// the collection.
type iteratorFn func() (interface{}, bool)

// generatorFn returns an iterator.
type generatorFn func() iteratorFn

// cancelFn should be called to clean up the goroutine that would otherwise leak.
type cancelFn func()

// mapperToIterator returns an iteratorFn version of a mappingFn. The second
// return value must be called at the end of iteration, or the underlying
// goroutine will leak.
func mapperToIterator(m mapperFn) (iteratorFn, cancelFn) {
	generatedValues := make(chan interface{}, 1)
	stopCh := make(chan interface{}, 1)
	go func() {
		m(func(obj interface{}) bool {
			select {
			case <-stopCh:
				return false
			case generatedValues <- obj:
				return true
			}
		})
		close(generatedValues)
	}()
	iter := func() (interface{}, bool) {
		value, ok := <-generatedValues
		return value, ok
	}
	return iter, func() {
		stopCh <- nil
	}
}

func intervalMapperToIterator(mapper func(IntervalReceiver)) (iter func() Interval, cancel func()) {
	genericMapper := func(m mapFn) {
		mapper(func(ival Interval) bool {
			return m(ival)
		})
	}

	genericIter, cancel := mapperToIterator(genericMapper)
	return func() Interval {
		genericVal, iterationEnded := genericIter()
		if !iterationEnded {
			return nil
		}
		ival, ok := genericVal.(Interval)
		if !ok {
			panic("unexpected value type, internal error")
		}
		return ival
	}, cancel
}

func setIntervalIterator(s SetInput, extent Interval) (iter func() Interval, cancel func()) {
	return intervalMapperToIterator(func(f IntervalReceiver) {
		s.IntervalsBetween(extent, f)
	})
}

// oldBehaviorFactory returns a nil interval. This was used before
// construction of a Set/ImmutableSet required passing in a factory method for
// creating a zero interval object.
var oldBehaviorFactory = makeIntervalFactor(func() Interval { return nil })

// intervalFactory is used to construct a zero-value interval. The zero value
// interval may be different for different types of intervals, so a factory is
// sometimes needed to write generic algorithms about intervals.
type intervalFactory struct {
	makeZero func() Interval
}

func makeIntervalFactor(makeZero func() Interval) intervalFactory {
	return intervalFactory{makeZero}
}
