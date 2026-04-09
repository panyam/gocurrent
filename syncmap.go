package gocurrent

import "sync"

// SyncMap is a type-safe generic wrapper around sync.Map.
//
// It provides the same concurrent map semantics as sync.Map — optimized for
// read-heavy workloads with stable keys — but with compile-time type safety
// instead of interface{} casts.
//
// For documentation on the underlying concurrency guarantees, see:
// https://pkg.go.dev/sync#Map
//
// Usage:
//
//	var m gocurrent.SyncMap[string, *Session]
//	m.Store("abc", session)
//	if s, ok := m.Load("abc"); ok {
//	    // s is *Session, no type assertion needed
//	}
type SyncMap[K comparable, V any] struct {
	m sync.Map
}

// Load returns the value stored in the map for a key, or the zero value if
// no value is present. The ok result indicates whether value was found.
func (m *SyncMap[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	if !ok {
		return value, false
	}
	return v.(V), true
}

// Store sets the value for a key.
func (m *SyncMap[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// Delete deletes the value for a key.
func (m *SyncMap[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// LoadAndDelete deletes the value for a key, returning the previous value
// if any. The loaded result reports whether the key was present.
func (m *SyncMap[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		return value, false
	}
	return v.(V), true
}

// LoadOrStore returns the existing value for the key if present. Otherwise,
// it stores and returns the given value. The loaded result is true if the
// value was loaded, false if stored.
func (m *SyncMap[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	v, loaded := m.m.LoadOrStore(key, value)
	return v.(V), loaded
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, Range stops the iteration.
//
// See sync.Map.Range for details on concurrent modification semantics.
func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(k, v any) bool {
		return f(k.(K), v.(V))
	})
}
