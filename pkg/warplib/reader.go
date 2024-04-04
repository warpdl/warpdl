package warplib

import "io"

type CallbackProxyReader struct {
	r io.Reader
	c func(n int)
}

func NewCallbackProxyReader(reader io.Reader, callback func(n int)) *CallbackProxyReader {
	return &CallbackProxyReader{
		r: reader,
		c: callback,
	}
}

func (p *CallbackProxyReader) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	p.c(n)
	return
}

type AsyncCallbackProxyReader struct {
	r io.Reader
	c func(n int)
}

func NewAsyncCallbackProxyReader(reader io.Reader, callback func(n int)) *AsyncCallbackProxyReader {
	return &AsyncCallbackProxyReader{
		r: reader,
		c: callback,
	}
}

func (p *AsyncCallbackProxyReader) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	go p.c(n)
	return
}
