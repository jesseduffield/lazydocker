package syncmap

import (
	"maps"
	"sync"
)

// A Map is a map of a string to a generified value which is locked for safe
// access from multiple threads.
// It is effectively a generic version of Golang's standard library sync.Map.
// Admittedly, that has optimizations for multithreading performance that we do
// not here; thus, Map should not be used in truly performance sensitive
// areas, but places where code cleanliness is more important than raw
// performance.
// Map must always be passed by reference, not by value, to ensure thread
// safety is maintained.
type Map[K comparable, V any] struct {
	data map[K]V
	lock sync.Mutex
}

// New generates a new, empty Map
func New[K comparable, V any]() *Map[K, V] {
	toReturn := new(Map[K, V])
	toReturn.data = make(map[K]V)

	return toReturn
}

// Put adds an entry into the map
func (m *Map[K, V]) Put(key K, value V) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.data[key] = value
}

// Get retrieves an entry from the map.
// Semantic match Golang map semantics - the bool represents whether the key
// exists, and the empty value of T will be returned if the key does not exist.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	value, exists := m.data[key]

	return value, exists
}

// Exists returns true if a key exists in the map.
func (m *Map[K, V]) Exists(key K) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, ok := m.data[key]

	return ok
}

// Delete removes an entry from the map.
func (m *Map[K, V]) Delete(key K) {
	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.data, key)
}

// ToMap returns a shallow copy of the underlying data of the Map.
func (m *Map[K, V]) ToMap() map[K]V {
	m.lock.Lock()
	defer m.lock.Unlock()

	return maps.Clone(m.data)
}

// Underlying returns a reference to the underlying storage of the Map.
// Once Underlying has been called, the Map is NO LONGER THREAD SAFE.
// If thread safety is still required, the shallow-copy offered by ToMap()
// should be used instead.
func (m *Map[K, V]) Underlying() map[K]V {
	return m.data
}
