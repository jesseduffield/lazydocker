package panels

import (
	"sort"
	"sync"
)

type FilteredList[T comparable] struct {
	allItems []T
	// indices of items in the allItems slice that are included in the filtered list
	indices []int

	mutex sync.RWMutex
}

func NewFilteredList[T comparable]() *FilteredList[T] {
	return &FilteredList[T]{}
}

func (self *FilteredList[T]) SetItems(items []T) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.allItems = items
	self.indices = make([]int, len(items))
	for i := range self.indices {
		self.indices[i] = i
	}
}

func (self *FilteredList[T]) Filter(filter func(T, int) bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.indices = self.indices[:0]
	for i, item := range self.allItems {
		if filter(item, i) {
			self.indices = append(self.indices, i)
		}
	}
}

func (self *FilteredList[T]) Sort(less func(T, T) bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if less == nil {
		return
	}

	sort.Slice(self.indices, func(i, j int) bool {
		return less(self.allItems[self.indices[i]], self.allItems[self.indices[j]])
	})
}

func (self *FilteredList[T]) Get(index int) T {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	return self.allItems[self.indices[index]]
}

func (self *FilteredList[T]) TryGet(index int) (T, bool) {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	if index < 0 || index >= len(self.indices) {
		var zero T
		return zero, false
	}

	return self.allItems[self.indices[index]], true
}

// returns the length of the filtered list
func (self *FilteredList[T]) Len() int {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	return len(self.indices)
}

func (self *FilteredList[T]) GetIndex(item T) int {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	for i, index := range self.indices {
		if self.allItems[index] == item {
			return i
		}
	}
	return -1
}

func (self *FilteredList[T]) GetItems() []T {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	result := make([]T, len(self.indices))
	for i, index := range self.indices {
		result[i] = self.allItems[index]
	}
	return result
}

func (self *FilteredList[T]) GetAllItems() []T {
	self.mutex.RLock()
	defer self.mutex.RUnlock()

	return self.allItems
}
