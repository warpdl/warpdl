package warplib

import (
	"io"
	"log"
	"sync"
)

// CallbackProxyReader wraps an io.Reader and invokes a callback function
// synchronously after each read operation with the number of bytes read.
type CallbackProxyReader struct {
	r io.Reader
	c func(n int)
}

// NewCallbackProxyReader creates a new CallbackProxyReader that wraps the given reader
// and calls the callback function synchronously after each read with the byte count.
func NewCallbackProxyReader(reader io.Reader, callback func(n int)) *CallbackProxyReader {
	return &CallbackProxyReader{
		r: reader,
		c: callback,
	}
}

// Read reads data from the underlying reader into b and invokes the callback
// synchronously with the number of bytes read.
func (p *CallbackProxyReader) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	p.c(n)
	return
}

// AsyncCallbackProxyReader wraps an io.Reader and invokes a callback function
// after each read.
//
// Historically this fired the callback in a goroutine per Read - on large
// resumes that produced thousands of goroutines for zero concurrency benefit.
// The callback is now invoked synchronously (protected against panics by
// the caller) and Wait remains a no-op for backward compatibility.
type AsyncCallbackProxyReader struct {
	r  io.Reader
	c  func(n int)
	wg sync.WaitGroup // kept for backward compatibility; never used.
	l  *log.Logger
}

// NewAsyncCallbackProxyReader creates an AsyncCallbackProxyReader. See the
// type-level comment for the behavior change compared with earlier versions.
func NewAsyncCallbackProxyReader(reader io.Reader, callback func(n int), logger *log.Logger) *AsyncCallbackProxyReader {
	return &AsyncCallbackProxyReader{
		r: reader,
		c: callback,
		l: logger,
	}
}

// Read reads data from the underlying reader into b and invokes the callback
// synchronously with the number of bytes read. A panic inside the callback
// is recovered so it cannot abort the read loop.
func (p *AsyncCallbackProxyReader) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	if p.c != nil {
		p.invokeCallback(n)
	}
	return
}

func (p *AsyncCallbackProxyReader) invokeCallback(n int) {
	defer func() {
		if r := recover(); r != nil && p.l != nil {
			p.l.Printf("async-callback-reader: panic in callback: %v", r)
		}
	}()
	p.c(n)
}

// Wait is retained for backward compatibility. Because callbacks now run
// synchronously, there is nothing to wait for; the call is a no-op.
func (p *AsyncCallbackProxyReader) Wait() {
	p.wg.Wait()
}
