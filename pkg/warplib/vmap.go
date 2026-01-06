package warplib

import (
	"sync"
)

// VMap is a thread-safe generic map with read-write mutex protection.
// It provides concurrent access to key-value pairs of any comparable key type.
type VMap[kT comparable, vT any] struct {
	kv map[kT]vT
	mu sync.RWMutex
}

// NewVMap creates and returns a new empty VMap instance with an initialized internal map.
func NewVMap[kT comparable, vT any]() VMap[kT, vT] {
	return VMap[kT, vT]{
		kv: make(map[kT]vT),
	}
}

// Make initializes the internal map. Call this to reset the map or if using a zero-value VMap.
func (vm *VMap[kT, vT]) Make() {
	vm.kv = make(map[kT]vT)
}

// Set stores a value for the given key with write lock protection.
func (vm *VMap[kT, vT]) Set(key kT, val vT) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.kv[key] = val
}

// GetUnsafe retrieves a value without lock protection. Use only when already holding a lock.
func (vm *VMap[kT, vT]) GetUnsafe(key kT) (val vT) {
	val = vm.kv[key]
	return
}

// Get retrieves a value for the given key with read lock protection.
func (vm *VMap[kT, vT]) Get(key kT) (val vT) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	val = vm.GetUnsafe(key)
	return
}

// Dump returns all keys and values as separate slices with write lock protection.
// RACE FIX: Acquire lock BEFORE reading len(vm.kv) to prevent concurrent modification.
func (vm *VMap[kT, vT]) Dump() (keys []kT, vals []vT) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	n := len(vm.kv)
	keys = make([]kT, n)
	vals = make([]vT, n)

	var i int
	for key, val := range vm.kv {
		keys[i] = key
		vals[i] = val
		i++
	}
	return
}

// Range iterates over all key-value pairs with read lock protection.
// The function f is called for each key-value pair. If f returns false,
// iteration stops early. The function f should not modify the map.
func (vm *VMap[kT, vT]) Range(f func(key kT, val vT) bool) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	for k, v := range vm.kv {
		if !f(k, v) {
			return
		}
	}
}

// Delete removes a key from the map with write lock protection.
// If the key does not exist, this is a no-op.
func (vm *VMap[kT, vT]) Delete(key kT) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	delete(vm.kv, key)
}
