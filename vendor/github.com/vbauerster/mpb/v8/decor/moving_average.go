package decor

import (
	"sort"
	"sync"

	"github.com/VividCortex/ewma"
)

var (
	_ ewma.MovingAverage = (*threadSafeMovingAverage)(nil)
	_ ewma.MovingAverage = (*medianWindow)(nil)
	_ sort.Interface     = (*medianWindow)(nil)
)

type threadSafeMovingAverage struct {
	ewma.MovingAverage
	mu sync.Mutex
}

func (s *threadSafeMovingAverage) Add(value float64) {
	s.mu.Lock()
	s.MovingAverage.Add(value)
	s.mu.Unlock()
}

func (s *threadSafeMovingAverage) Value() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.MovingAverage.Value()
}

func (s *threadSafeMovingAverage) Set(value float64) {
	s.mu.Lock()
	s.MovingAverage.Set(value)
	s.mu.Unlock()
}

// NewThreadSafeMovingAverage converts provided ewma.MovingAverage
// into thread safe ewma.MovingAverage.
func NewThreadSafeMovingAverage(average ewma.MovingAverage) ewma.MovingAverage {
	if tsma, ok := average.(*threadSafeMovingAverage); ok {
		return tsma
	}
	return &threadSafeMovingAverage{MovingAverage: average}
}

type medianWindow [3]float64

func (s *medianWindow) Len() int           { return len(s) }
func (s *medianWindow) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s *medianWindow) Less(i, j int) bool { return s[i] < s[j] }

func (s *medianWindow) Add(value float64) {
	s[0], s[1] = s[1], s[2]
	s[2] = value
}

func (s *medianWindow) Value() float64 {
	tmp := *s
	sort.Sort(&tmp)
	return tmp[1]
}

func (s *medianWindow) Set(value float64) {
	for i := 0; i < len(s); i++ {
		s[i] = value
	}
}

// NewMedian is fixed last 3 samples median MovingAverage.
func NewMedian() ewma.MovingAverage {
	return new(medianWindow)
}
