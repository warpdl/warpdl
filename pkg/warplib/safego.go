package warplib

import (
	"log"
	"runtime/debug"
	"sync"
)

// safeGo runs fn in a goroutine with panic recovery.
// If wg is non-nil, it's decremented on completion (normal or panic).
// If logger is non-nil, panics are logged with stack traces.
// If onPanic is non-nil, it's called with the recovered value.
func safeGo(l *log.Logger, wg *sync.WaitGroup, context string, onPanic func(r interface{}), fn func()) {
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		defer func() {
			if r := recover(); r != nil {
				if l != nil {
					l.Printf("PANIC [%s]: %v\n%s", context, r, debug.Stack())
				}
				if onPanic != nil {
					onPanic(r)
				}
			}
		}()
		fn()
	}()
}
