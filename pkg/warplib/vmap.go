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

// Make initializes the internal map. Safe to call concurrently with Set/Get/Range/Delete.
// Make is idempotent: if the map is already initialized, it is a no-op. Callers that
// want to explicitly reset the map should use Reset instead.
func (vm *VMap[kT, vT]) Make() {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if vm.kv == nil {
		vm.kv = make(map[kT]vT)
	}
}

// Reset clears the map and replaces it with a freshly allocated one.
// Safe to call concurrently with other operations.
func (vm *VMap[kT, vT]) Reset() {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.kv = make(map[kT]vT)
}

// Len returns the number of entries currently in the map.
func (vm *VMap[kT, vT]) Len() int {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return len(vm.kv)
}

// Set stores a value for the given key with write lock protection.
func (vm *VMap[kT, vT]) Set(key kT, val vT) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if vm.kv == nil {
		vm.kv = make(map[kT]vT)
	}
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

// Dump returns all keys and values as separate slices.
// Uses a read lock since the internal map is not mutated.
func (vm *VMap[kT, vT]) Dump() (keys []kT, vals []vT) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

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

// Range iterates over all key-value pairs.
// Because f is invoked on a snapshot of entries taken under the read lock and
// then released, callers may safely perform blocking work inside f (including
// map mutations on the same VMap) without starving writers.
// The function f is called for each key-value pair. If f returns false,
// iteration stops early.
func (vm *VMap[kT, vT]) Range(f func(key kT, val vT) bool) {
	keys, vals := vm.Dump()
	for i := range keys {
		if !f(keys[i], vals[i]) {
			return
		}
	}
}

// RangeLocked iterates over entries while holding the read lock for the
// entire duration. Use this variant only when f is fast (no I/O, no locks)
// and zero-copy iteration is required.
func (vm *VMap[kT, vT]) RangeLocked(f func(key kT, val vT) bool) {
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
