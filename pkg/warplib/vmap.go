package warplib

import (
	"sync"
)

type VMap[kT comparable, vT any] struct {
	kv map[kT]vT
	mu sync.RWMutex
}

func NewVMap[kT comparable, vT any]() VMap[kT, vT] {
	return VMap[kT, vT]{
		kv: make(map[kT]vT),
	}
}

func (vm *VMap[kT, vT]) Make() {
	vm.kv = make(map[kT]vT)
}

func (vm *VMap[kT, vT]) Set(key kT, val vT) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.kv[key] = val
}

func (vm *VMap[kT, vT]) GetUnsafe(key kT) (val vT) {
	val = vm.kv[key]
	return
}

func (vm *VMap[kT, vT]) Get(key kT) (val vT) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	val = vm.GetUnsafe(key)
	return
}

func (vm *VMap[kT, vT]) Dump() (keys []kT, vals []vT) {
	n := len(vm.kv)

	keys = make([]kT, n)
	vals = make([]vT, n)

	vm.mu.Lock()
	defer vm.mu.Unlock()

	var i int
	for key, val := range vm.kv {
		keys[i] = key
		vals[i] = val
		i++
	}
	return
}
