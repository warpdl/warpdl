package warplib

import "io"

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
// asynchronously in a goroutine after each read operation with the number of bytes read.
type AsyncCallbackProxyReader struct {
	r io.Reader
	c func(n int)
}

// NewAsyncCallbackProxyReader creates a new AsyncCallbackProxyReader that wraps the given reader
// and calls the callback function asynchronously in a goroutine after each read with the byte count.
func NewAsyncCallbackProxyReader(reader io.Reader, callback func(n int)) *AsyncCallbackProxyReader {
	return &AsyncCallbackProxyReader{
		r: reader,
		c: callback,
	}
}

// Read reads data from the underlying reader into b and invokes the callback
// asynchronously in a goroutine with the number of bytes read.
func (p *AsyncCallbackProxyReader) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	go p.c(n)
	return
}
