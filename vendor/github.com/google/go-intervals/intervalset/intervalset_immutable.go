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

package intervalset

// ImmutableSet is a set of interval objects. It provides various set theory
// operations.
type ImmutableSet struct {
	set *Set
}

// NewImmutableSet returns a new set given a sorted slice of intervals. This
// function panics if the intervals are not sorted.
func NewImmutableSet(intervals []Interval) *ImmutableSet {
	return NewImmutableSetV1(intervals, oldBehaviorFactory.makeZero)
}

// NewImmutableSetV1 returns a new set given a sorted slice of intervals. This
// function panics if the intervals are not sorted.
func NewImmutableSetV1(intervals []Interval, makeZero func() Interval) *ImmutableSet {
	return &ImmutableSet{NewSetV1(intervals, makeZero)}
}

// String returns a human-friendly representation of the set.
func (s *ImmutableSet) String() string {
	return s.set.String()
}

// Extent returns the Interval defined by the minimum and maximum values of the
// set.
func (s *ImmutableSet) Extent() Interval {
	return s.set.Extent()
}

// Contains reports whether an interval is entirely contained by the set.
func (s *ImmutableSet) Contains(ival Interval) bool {
	return s.set.Contains(ival)
}

// Union returns a set with the contents of this set and another set.
func (s *ImmutableSet) Union(b SetInput) *ImmutableSet {
	union := s.set.Copy()
	union.Add(b)
	return &ImmutableSet{union}
}

// Sub returns a set without the intervals of another set.
func (s *ImmutableSet) Sub(b SetInput) *ImmutableSet {
	x := s.set.Copy()
	x.Sub(b)
	return &ImmutableSet{x}
}

// Intersect returns the intersection of two sets.
func (s *ImmutableSet) Intersect(b SetInput) *ImmutableSet {
	x := s.set.Copy()
	x.Intersect(b)
	return &ImmutableSet{x}
}

// IntervalsBetween iterates over the intervals within extents set and calls f
// with each. If f returns false, iteration ceases.
//
// Any interval within the set that overlaps partially with extents is truncated
// before being passed to f.
func (s *ImmutableSet) IntervalsBetween(extents Interval, f IntervalReceiver) {
	s.set.IntervalsBetween(extents, f)
}

// Intervals iterates over all the intervals within the set and calls f with
// each one. If f returns false, iteration ceases.
func (s *ImmutableSet) Intervals(f IntervalReceiver) {
	s.set.Intervals(f)
}
